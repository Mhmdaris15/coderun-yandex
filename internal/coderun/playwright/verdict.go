package pwclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"coderun-agent/internal/coderun"
)

type fileRef struct {
	Link string `json:"link"`
	Size int64  `json:"size"`
}

type detailPayload struct {
	Result *struct {
		GlobalID            string `json:"globalId"`
		Verdict             string `json:"verdict"`
		Status              string `json:"status"`
		SubmitAt            string `json:"submitAt"`
		MaxMemoryUsageBytes int64  `json:"maxMemoryUsageBytes"`
		MaxTimeUsageMillis  int    `json:"maxTimeUsageMillis"`
		FirstFailedTest     int    `json:"firstFailedTestNumber"`
		CompileLog          string `json:"compileLog"`
		Compiler            struct {
			Slug string `json:"slug"`
		} `json:"compiler"`
		OpenTests struct {
			TotalTests int `json:"totalTests"`
			Tests      []struct {
				TestNumber      int     `json:"testNumber"`
				UsedTimeMillis  int     `json:"usedTimeMillis"`
				UsedMemoryBytes int64   `json:"usedMemoryBytes"`
				IsSample        bool    `json:"isSample"`
				Verdict         string  `json:"verdict"`
				Input           fileRef `json:"input"`
				Output          fileRef `json:"output"`
				Answer          fileRef `json:"answer"`
			} `json:"tests"`
		} `json:"openTests"`
		HiddenTests struct {
			TotalTests int `json:"totalTests"`
		} `json:"hiddenTests"`
		RuntimeLimits struct {
			TimeLimitMillis  int   `json:"timeLimitMillis"`
			MemoryLimitBytes int64 `json:"memoryLimitBytes"`
		} `json:"runtimeLimits"`
	} `json:"result"`
	Error *struct {
		StatusCode int    `json:"statusCode"`
		Code       string `json:"code"`
		Message    string `json:"message"`
	} `json:"error"`
}

// ParseSubmissionDetail decodes GET /api/submission/<globalId>.
//
// It returns the submission plus the presigned links for the first failing
// sample test, in input/output/answer order. Those links are returned rather
// than stored: they are bearer credentials valid for 12 hours and must never
// be persisted or logged.
func ParseSubmissionDetail(body []byte) (*coderun.Submission, []string, error) {
	var p detailPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, nil, fmt.Errorf("decode submission detail: %w", err)
	}
	if p.Error != nil {
		return nil, nil, fmt.Errorf("submission detail: %s (%s, status %d)",
			p.Error.Message, p.Error.Code, p.Error.StatusCode)
	}
	if p.Result == nil {
		return nil, nil, fmt.Errorf("submission detail carried no result")
	}
	r := p.Result

	sub := &coderun.Submission{
		GlobalID:         r.GlobalID,
		CompilerSlug:     r.Compiler.Slug,
		Status:           r.Status,
		Verdict:          r.Verdict,
		MaxTimeMillis:    r.MaxTimeUsageMillis,
		MaxMemoryBytes:   r.MaxMemoryUsageBytes,
		FirstFailedTest:  r.FirstFailedTest,
		CompileLog:       r.CompileLog,
		TimeLimitMillis:  r.RuntimeLimits.TimeLimitMillis,
		MemoryLimitBytes: r.RuntimeLimits.MemoryLimitBytes,
		HiddenTestCount:  r.HiddenTests.TotalTests,
	}
	if t, err := time.Parse(time.RFC3339Nano, r.SubmitAt); err == nil {
		sub.SubmittedAt = t
	}

	var links []string
	for _, t := range r.OpenTests.Tests {
		sub.OpenTests = append(sub.OpenTests, coderun.TestResult{
			Number:      t.TestNumber,
			Verdict:     t.Verdict,
			IsSample:    t.IsSample,
			TimeMillis:  t.UsedTimeMillis,
			MemoryBytes: t.UsedMemoryBytes,
		})
		// Only the first failing test's artifacts are needed: it is what a
		// correction step would reason about.
		if links == nil && !coderun.IsAccepted(t.Verdict) && t.Verdict != "" {
			links = []string{t.Input.Link, t.Output.Link, t.Answer.Link}
		}
	}
	return sub, links, nil
}

func (b *Browser) GetSubmission(ctx context.Context, globalID string) (*coderun.Submission, error) {
	body, err := b.apiGet(ctx, "/api/submission/"+globalID)
	if err != nil {
		return nil, err
	}
	sub, _, err := ParseSubmissionDetail(body)
	return sub, err
}

// AwaitVerdict polls until judging finishes. It observes API state rather than
// reloading the page, per the design's rate-limiting rules.
func (b *Browser) AwaitVerdict(ctx context.Context, globalID string, interval, timeout time.Duration) (*coderun.Submission, error) {
	deadline := time.Now().Add(timeout)

	for {
		body, err := b.apiGet(ctx, "/api/submission/"+globalID)
		if err != nil {
			return nil, err
		}
		sub, links, err := ParseSubmissionDetail(body)
		if err != nil {
			return nil, err
		}

		if coderun.IsFinished(sub.Status) {
			if !coderun.IsAccepted(sub.Verdict) && !knownVerdict(sub.Verdict) {
				// Not an error — but a verdict we have never seen is worth a
				// loud line, because the open-set rule just classified it as
				// a failure.
				slog.Warn("unrecognised verdict; treated as not accepted",
					"verdict", sub.Verdict, "submission", globalID)
			}
			if len(links) > 0 {
				if err := b.FetchTestArtifacts(ctx, sub, links, b.maxArtifact); err != nil {
					slog.Warn("could not fetch failing-test artifacts", "error", err)
				}
			}
			return sub, nil
		}

		if time.Now().After(deadline) {
			return sub, fmt.Errorf("timed out after %s waiting for a verdict (last status %q)", timeout, sub.Status)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// knownVerdict lists verdicts observed or strongly expected. It exists only to
// decide whether to log a warning — never to decide success.
func knownVerdict(v string) bool {
	switch v {
	case "WRONG_ANSWER", "TIME_LIMIT_EXCEEDED", "MEMORY_LIMIT_EXCEEDED",
		"RUNTIME_ERROR", "COMPILATION_ERROR", "PRESENTATION_ERROR":
		return true
	}
	return false
}

// FetchTestArtifacts downloads the failing sample test's input, actual output
// and expected answer. Links expire after 12 hours, so this runs immediately
// and the links themselves are never stored.
func (b *Browser) FetchTestArtifacts(ctx context.Context, sub *coderun.Submission, links []string, maxBytes int64) error {
	if len(sub.OpenTests) == 0 || len(links) < 3 {
		return nil
	}
	fields := []*string{}
	for i := range sub.OpenTests {
		if !coderun.IsAccepted(sub.OpenTests[i].Verdict) && sub.OpenTests[i].Verdict != "" {
			t := &sub.OpenTests[i]
			fields = []*string{&t.Input, &t.Output, &t.Answer}
			break
		}
	}
	if len(fields) != 3 {
		return nil
	}

	for i, link := range links[:3] {
		if link == "" {
			continue
		}
		text, err := b.fetchArtifact(ctx, link, maxBytes)
		if err != nil {
			return fmt.Errorf("artifact %d: %w", i, err)
		}
		*fields[i] = text
	}
	return nil
}

func (b *Browser) fetchArtifact(ctx context.Context, link string, maxBytes int64) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	// Note: the URL is deliberately absent from this log line, and from every
	// error below. Presigned links are 12-hour bearer credentials, and
	// playwright-go's transport errors embed the request URL in their own
	// message text — so those errors must never be wrapped or logged
	// verbatim, only reported as a sanitised, fixed message.
	slog.Debug("fetching test artifact", "url", "<presigned>")

	resp, err := b.Ctx.Request().Get(link)
	if err != nil {
		return "", errors.New("fetch <presigned>: request failed")
	}
	defer resp.Dispose()

	if resp.Status() != 200 {
		return "", fmt.Errorf("fetch <presigned>: status %d", resp.Status())
	}
	raw, err := resp.Body()
	if err != nil {
		return "", errors.New("fetch <presigned>: could not read response body")
	}
	if int64(len(raw)) > maxBytes {
		raw = raw[:maxBytes]
	}
	return string(raw), nil
}
