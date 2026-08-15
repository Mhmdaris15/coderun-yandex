package pwclient

import (
	"strings"
	"testing"
)

func TestValidateSourceAcceptsNormalFile(t *testing.T) {
	if err := ValidateSource([]byte("def solution(n, a):\n    return 0\n"), 262144); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateSourceRejectsEmpty(t *testing.T) {
	if err := ValidateSource(nil, 262144); err == nil {
		t.Fatal("expected an error for an empty source file")
	}
}

func TestValidateSourceRejectsOversize(t *testing.T) {
	// CodeRun's upload modal states a 256 KB limit. Catch it locally rather
	// than letting the site reject the upload.
	big := make([]byte, 262145)
	for i := range big {
		big[i] = 'x'
	}
	err := ValidateSource(big, 262144)
	if err == nil {
		t.Fatal("expected an error for a file above the 256 KB limit")
	}
	if !strings.Contains(err.Error(), "262144") {
		t.Errorf("error should state the limit, got %v", err)
	}
}

func TestParseSubmitResponse(t *testing.T) {
	// Captured verbatim from a real POST /api/submission/submit response.
	body := []byte(`{"result":{"id":2338530,` +
		`"globalId":"1001006a-7f48-105c-b62a-db256d869399","status":"PENDING",` +
		`"source":[{"key":"coding/solutions/probe.py","bucket":"contest-hidden",` +
		`"contentType":"text/x-python","size":65,"fileName":"probe.py"}]},"error":null}`)

	globalID, status, err := ParseSubmitResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if globalID != "1001006a-7f48-105c-b62a-db256d869399" {
		t.Errorf("globalID = %q", globalID)
	}
	if status != "PENDING" {
		t.Errorf("status = %q, want PENDING", status)
	}
}

func TestParseSubmitResponseSurfacesAPIError(t *testing.T) {
	body := []byte(`{"result":null,"error":{"statusCode":403,"code":"bad-csrf","message":"Invalid CSRF Token"}}`)
	if _, _, err := ParseSubmitResponse(body); err == nil {
		t.Fatal("expected an error when the API returns one")
	}
}

func TestParseSubmitResponseRejectsMissingGlobalID(t *testing.T) {
	// A submission we cannot identify must never be reported as successful:
	// we would have no way to poll it and might blindly resubmit.
	body := []byte(`{"result":{"id":1,"status":"PENDING"},"error":null}`)
	if _, _, err := ParseSubmitResponse(body); err == nil {
		t.Fatal("expected an error when globalId is absent")
	}
}
