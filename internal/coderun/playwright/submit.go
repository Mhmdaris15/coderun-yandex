package pwclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"coderun-agent/internal/coderun"
)

// ErrSubmitUnconfirmed means the confirming click may have landed on
// CodeRun's servers, but no response was captured to prove it and the
// latest-submission fallback could not recover a globalId either. A real
// submission may exist; callers must never resubmit to "fix" this.
var ErrSubmitUnconfirmed = errors.New("submit unconfirmed: a submission may exist on CodeRun but was not verified locally")

// errSubmitBodyUnparseable marks a decode failure in ParseSubmitResponse as
// ambiguous rather than a confirmed rejection: the confirming click reached
// the server (a response came back), but nothing about its outcome could be
// read from it. Submit treats this the same as a missed response — through
// ErrSubmitUnconfirmed — rather than as a confirmed failure with nothing to
// recover.
var errSubmitBodyUnparseable = errors.New("submit response body could not be decoded")

// ValidateSource enforces CodeRun's stated upload limit locally.
func ValidateSource(source []byte, maxBytes int64) error {
	if len(source) == 0 {
		return fmt.Errorf("source file is empty")
	}
	if int64(len(source)) > maxBytes {
		return fmt.Errorf("source file is %d bytes, limit is %d", len(source), maxBytes)
	}
	return nil
}

// ParseSubmitResponse reads the POST /api/submission/submit response.
func ParseSubmitResponse(body []byte) (string, string, error) {
	var payload struct {
		Result *struct {
			GlobalID string `json:"globalId"`
			Status   string `json:"status"`
		} `json:"result"`
		Error *struct {
			StatusCode int    `json:"statusCode"`
			Code       string `json:"code"`
			Message    string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", "", fmt.Errorf("%w: %v", errSubmitBodyUnparseable, err)
	}
	if payload.Error != nil {
		return "", "", fmt.Errorf("submit rejected: %s (%s, status %d)",
			payload.Error.Message, payload.Error.Code, payload.Error.StatusCode)
	}
	if payload.Result == nil || payload.Result.GlobalID == "" {
		return "", "", fmt.Errorf("submit response carried no globalId; cannot track this submission")
	}
	return payload.Result.GlobalID, payload.Result.Status, nil
}

// Submit uploads a source file through the attach-file modal.
//
// The response listener is registered before the confirming click so the
// globalId is captured exactly, with no race against the redirect that
// follows. It returns the exact bytes it read from sourcePath alongside the
// submission (whenever it got far enough to read them), so a caller
// archiving an attempt artifact records precisely what was uploaded rather
// than a second, possibly different, read of the same path.
func (b *Browser) Submit(ctx context.Context, ref coderun.ProblemRef, compilerSlug, sourcePath string) (*coderun.Submission, []byte, error) {
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		return nil, nil, fmt.Errorf("read source: %w", err)
	}
	if err := ValidateSource(source, b.maxSource); err != nil {
		return nil, nil, err
	}

	if _, err := b.Goto(ctx, ProblemPath(ref, compilerSlug)); err != nil {
		return nil, nil, err
	}

	if err := b.Page.GetByTestId("file-attach").Click(); err != nil {
		return nil, nil, fmt.Errorf("open the upload dialog: %w", err)
	}

	// Stage the exact bytes just validated into a fresh temp file and point
	// the browser at that, rather than at sourcePath again. This is the only
	// file the browser ever reads for the upload, so what is archived below
	// as "source" is guaranteed to match exactly what was sent, even if
	// sourcePath is edited on disk between validation and upload.
	stageDir, err := os.MkdirTemp("", "coderun-submit-*")
	if err != nil {
		return nil, nil, fmt.Errorf("create upload staging directory: %w", err)
	}
	defer os.RemoveAll(stageDir)

	stagedPath := filepath.Join(stageDir, filepath.Base(sourcePath))
	if err := os.WriteFile(stagedPath, source, 0o644); err != nil {
		return nil, nil, fmt.Errorf("stage source for upload: %w", err)
	}

	input := b.Page.Locator(`[data-testid="file-attach-modal"] input[type=file]`)
	if err := input.SetInputFiles([]string{stagedPath}); err != nil {
		return nil, nil, fmt.Errorf("attach %s: %w", sourcePath, err)
	}

	submittedAt := time.Now().UTC()

	resp, err := b.Page.ExpectResponse("**/api/submission/submit", func() error {
		return b.Page.GetByTestId("confirm").Click()
	})
	if err != nil {
		sub, subErr := b.unconfirmedSubmission(ctx, ref, compilerSlug, submittedAt, err)
		return sub, source, subErr
	}

	body, err := resp.Body()
	if err != nil {
		// The click landed and a response object came back, but its body
		// could not be read. This is the same ambiguous window as a missed
		// response: a submission may exist server-side with nothing local to
		// prove it, so it must not vanish silently.
		sub, subErr := b.unconfirmedSubmission(ctx, ref, compilerSlug, submittedAt,
			fmt.Errorf("read submit response: %w", err))
		return sub, source, subErr
	}
	globalID, status, err := ParseSubmitResponse(body)
	if err != nil {
		if errors.Is(err, errSubmitBodyUnparseable) {
			sub, subErr := b.unconfirmedSubmission(ctx, ref, compilerSlug, submittedAt, err)
			return sub, source, subErr
		}
		// Any other ParseSubmitResponse error (an explicit API rejection, or
		// a response with neither an error nor a globalId) is a conclusive
		// signal read straight from a fully-parsed response, not an ambiguous
		// one — so it is reported as-is rather than routed through the
		// unconfirmed/recovery path.
		return nil, source, err
	}

	slog.Info("submitted", "problem", ref.ProblemSlug, "compiler", compilerSlug, "submission", globalID)

	return &coderun.Submission{
		GlobalID:     globalID,
		Ref:          ref,
		CompilerSlug: compilerSlug,
		Status:       status,
		SubmittedAt:  submittedAt,
	}, source, nil
}

// unconfirmedSubmission handles every path where the confirming click may
// have reached CodeRun's servers but the outcome could not be verified
// locally — whether the response was never captured, or one came back but
// its body could not be read or decoded. It never retries the click (a blind
// resubmit could double-submit); instead it asks CodeRun for the latest
// submission on this problem/compiler, accepting it only if it postdates the
// click.
func (b *Browser) unconfirmedSubmission(ctx context.Context, ref coderun.ProblemRef, compilerSlug string, submittedAt time.Time, cause error) (*coderun.Submission, error) {
	if globalID, ok := b.recoverLatestSubmission(ctx, ref, compilerSlug, submittedAt); ok {
		slog.Warn("submit response missed; recovered globalId from the latest-submission fallback",
			"submission", globalID)
		return &coderun.Submission{
			GlobalID:     globalID,
			Ref:          ref,
			CompilerSlug: compilerSlug,
			SubmittedAt:  submittedAt,
		}, nil
	}
	return &coderun.Submission{
			Ref:          ref,
			CompilerSlug: compilerSlug,
			Status:       "UNCONFIRMED",
			SubmittedAt:  submittedAt,
		}, fmt.Errorf("%w: submit sent but the response was not captured (do not retry blindly): %v",
			ErrSubmitUnconfirmed, cause)
}

// recoverLatestSubmission asks CodeRun for its latest submission on this
// problem/compiler and reports its globalId, but only if it can be confirmed
// as ours (submitted at or after sinceClick). The endpoint returns *a* latest
// submission, not necessarily the one just submitted, so anything older is
// rejected rather than trusted.
func (b *Browser) recoverLatestSubmission(ctx context.Context, ref coderun.ProblemRef, compilerSlug string, sinceClick time.Time) (string, bool) {
	path := fmt.Sprintf("/api/submission/latest?compilerSlug=%s&problemSlug=%s",
		url.QueryEscape(compilerSlug), url.QueryEscape(ref.ProblemSlug))
	body, err := b.apiGet(ctx, path)
	if err != nil {
		slog.Warn("latest-submission fallback failed", "error", err)
		return "", false
	}
	return parseLatestSubmission(body, sinceClick)
}

// parseLatestSubmission decodes GET /api/submission/latest and returns the
// globalId of result.latestSubmission, but only when its submitAt parses and
// is at or after sinceClick. A null/absent latestSubmission, an unparsable
// submitAt, or a submitAt older than the click all report ok=false: the
// caller must not mistake someone else's earlier submission for this one.
func parseLatestSubmission(body []byte, sinceClick time.Time) (globalID string, ok bool) {
	var payload struct {
		Result *struct {
			LatestSubmission *struct {
				GlobalID string `json:"globalId"`
				SubmitAt string `json:"submitAt"`
			} `json:"latestSubmission"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", false
	}
	if payload.Result == nil || payload.Result.LatestSubmission == nil {
		return "", false
	}
	ls := payload.Result.LatestSubmission
	if ls.GlobalID == "" {
		return "", false
	}
	t, err := time.Parse(time.RFC3339Nano, ls.SubmitAt)
	if err != nil {
		return "", false
	}
	if t.Before(sinceClick) {
		return "", false
	}
	return ls.GlobalID, true
}
