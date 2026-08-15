package pwclient

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"coderun-agent/internal/coderun"
)

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
		return "", "", fmt.Errorf("decode submit response: %w", err)
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
// follows.
func (b *Browser) Submit(ctx context.Context, ref coderun.ProblemRef, compilerSlug, sourcePath string) (*coderun.Submission, error) {
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("read source: %w", err)
	}
	if err := ValidateSource(source, b.maxSource); err != nil {
		return nil, err
	}

	if _, err := b.Goto(ctx, ProblemPath(ref, compilerSlug)); err != nil {
		return nil, err
	}

	if err := b.Page.GetByTestId("file-attach").Click(); err != nil {
		return nil, fmt.Errorf("open the upload dialog: %w", err)
	}

	input := b.Page.Locator(`[data-testid="file-attach-modal"] input[type=file]`)
	if err := input.SetInputFiles([]string{sourcePath}); err != nil {
		return nil, fmt.Errorf("attach %s: %w", sourcePath, err)
	}

	submittedAt := time.Now().UTC()

	resp, err := b.Page.ExpectResponse("**/api/submission/submit", func() error {
		return b.Page.GetByTestId("confirm").Click()
	})
	if err != nil {
		// The click may have landed even though the response was missed. Do
		// not retry: a blind resubmit could double-submit.
		return nil, fmt.Errorf("submit sent but the response was not captured (do not retry blindly): %w", err)
	}

	body, err := resp.Body()
	if err != nil {
		return nil, fmt.Errorf("read submit response: %w", err)
	}
	globalID, status, err := ParseSubmitResponse(body)
	if err != nil {
		return nil, err
	}

	slog.Info("submitted", "problem", ref.ProblemSlug, "compiler", compilerSlug, "submission", globalID)

	return &coderun.Submission{
		GlobalID:     globalID,
		Ref:          ref,
		CompilerSlug: compilerSlug,
		Status:       status,
		SubmittedAt:  submittedAt,
	}, nil
}
