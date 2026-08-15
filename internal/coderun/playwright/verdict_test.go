package pwclient

import (
	"testing"

	"coderun-agent/internal/coderun"
)

// Captured verbatim from GET /api/submission/<globalId>, with the presigned
// URLs shortened. Presigned links are bearer credentials and are never stored.
const detailJSON = `{"result":{
 "globalId":"1001006a-7f48-105c-b62a-db256d869399",
 "verdict":"WRONG_ANSWER","status":"FINISHED",
 "submitAt":"2026-08-14T16:53:35.989206Z",
 "compiler":{"slug":"python_make","title":"Python","version":"3.12.3"},
 "maxMemoryUsageBytes":3620864,"maxTimeUsageMillis":23,
 "firstFailedTestNumber":1,"compileLog":null,
 "openTests":{"totalTests":2,"tests":[
   {"testNumber":1,"usedTimeMillis":23,"usedMemoryBytes":3620864,
    "isSample":true,"verdict":"WRONG_ANSWER",
    "input":{"link":"https://s3/input","size":13},
    "output":{"link":"https://s3/output","size":2},
    "answer":{"link":"https://s3/answer","size":2}}]},
 "hiddenTests":{"totalTests":83,"tests":[]},
 "runtimeLimits":{"timeLimitMillis":2000,"memoryLimitBytes":268435456},
 "isExpired":false},"error":null}`

func TestParseSubmissionDetail(t *testing.T) {
	sub, links, err := ParseSubmissionDetail([]byte(detailJSON))
	if err != nil {
		t.Fatal(err)
	}

	if sub.Verdict != "WRONG_ANSWER" {
		t.Errorf("Verdict = %q", sub.Verdict)
	}
	if sub.Status != "FINISHED" {
		t.Errorf("Status = %q", sub.Status)
	}
	if sub.MaxTimeMillis != 23 {
		t.Errorf("MaxTimeMillis = %d, want 23", sub.MaxTimeMillis)
	}
	if sub.MaxMemoryBytes != 3620864 {
		t.Errorf("MaxMemoryBytes = %d", sub.MaxMemoryBytes)
	}
	if sub.FirstFailedTest != 1 {
		t.Errorf("FirstFailedTest = %d, want 1", sub.FirstFailedTest)
	}
	if sub.HiddenTestCount != 83 {
		t.Errorf("HiddenTestCount = %d, want 83", sub.HiddenTestCount)
	}
	if sub.TimeLimitMillis != 2000 || sub.MemoryLimitBytes != 268435456 {
		t.Errorf("runtime limits = %d ms / %d bytes", sub.TimeLimitMillis, sub.MemoryLimitBytes)
	}
	if len(sub.OpenTests) != 1 {
		t.Fatalf("got %d open tests, want 1", len(sub.OpenTests))
	}
	if !sub.OpenTests[0].IsSample {
		t.Error("open test should be marked as a sample")
	}
	if len(links) != 3 {
		t.Fatalf("got %d artifact links, want 3 (input, output, answer)", len(links))
	}
	if links[0] != "https://s3/input" || links[2] != "https://s3/answer" {
		t.Errorf("links out of order: %v", links)
	}
}

func TestParseSubmissionDetailPending(t *testing.T) {
	body := []byte(`{"result":{"globalId":"g","status":"PENDING","verdict":""},"error":null}`)
	sub, links, err := ParseSubmissionDetail(body)
	if err != nil {
		t.Fatal(err)
	}
	if coderun.IsFinished(sub.Status) {
		t.Error("PENDING must not be reported as finished")
	}
	if len(links) != 0 {
		t.Error("a pending submission has no artifacts to fetch")
	}
}

func TestIsAcceptedIsClosedOnSuccessOnly(t *testing.T) {
	// The open-set rule: only known-success verdicts pass. Everything else,
	// including verdicts nobody has seen yet, is failure.
	for _, v := range []string{"OK", "ACCEPTED"} {
		if !coderun.IsAccepted(v) {
			t.Errorf("IsAccepted(%q) = false, want true", v)
		}
	}
	for _, v := range []string{
		"WRONG_ANSWER", "TIME_LIMIT_EXCEEDED", "COMPILATION_ERROR",
		"RUNTIME_ERROR", "SOMETHING_NOBODY_HAS_SEEN_YET", "", "ok", "Accepted",
	} {
		if coderun.IsAccepted(v) {
			t.Errorf("IsAccepted(%q) = true — unknown verdicts must never count as success", v)
		}
	}
}

func TestParseSubmissionDetailSurfacesAPIError(t *testing.T) {
	body := []byte(`{"result":null,"error":{"statusCode":404,"code":"not-found","message":"nope"}}`)
	if _, _, err := ParseSubmissionDetail(body); err == nil {
		t.Fatal("expected an error when the API returns one")
	}
}
