package pwclient

import (
	"strings"
	"testing"

	"coderun-agent/internal/coderun"
)

func TestProblemPath(t *testing.T) {
	ref := coderun.ProblemRef{SelectionSlug: "2025-summer-common", ProblemSlug: "bridge-to-the-palace"}

	if got, want := ProblemPath(ref, ""), "/selections/2025-summer-common/problems/bridge-to-the-palace"; got != want {
		t.Errorf("ProblemPath() = %q, want %q", got, want)
	}
	// Language is selected by URL parameter rather than by clicking the
	// dropdown: fewer interactions, and it is idempotent.
	want := "/selections/2025-summer-common/problems/bridge-to-the-palace?compiler=python_make"
	if got := ProblemPath(ref, "python_make"); got != want {
		t.Errorf("ProblemPath() = %q, want %q", got, want)
	}
}

func TestParseContextID(t *testing.T) {
	url := "https://coderun.yandex.ru/api/problem/bridge-to-the-palace/solution-template?compilerSlug=dart_make&problemContextId=1838"
	got, err := ParseContextID(url)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1838 {
		t.Errorf("ParseContextID() = %d, want 1838", got)
	}
}

func TestParseContextIDMissing(t *testing.T) {
	if _, err := ParseContextID("https://coderun.yandex.ru/api/problem/x/solution-template?compilerSlug=go_make"); err == nil {
		t.Fatal("expected an error when problemContextId is absent")
	}
}

func TestParseContextIDNotANumber(t *testing.T) {
	if _, err := ParseContextID("https://x/?problemContextId=abc"); err == nil {
		t.Fatal("expected an error for a non-numeric id")
	}
}

func TestParseTemplateResponse(t *testing.T) {
	body := []byte(`{"result":{"content":"def solution(n, a):\n    pass\n"},"error":null}`)
	got, err := ParseTemplateResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if got != "def solution(n, a):\n    pass\n" {
		t.Errorf("content = %q", got)
	}
}

func TestParseTemplateResponseSurfacesAPIError(t *testing.T) {
	// Without checking the error object, this looks identical to an empty
	// (but successful) template.
	body := []byte(`{"result":null,"error":{"statusCode":404,"code":"not-found","message":"no such problem"}}`)
	_, err := ParseTemplateResponse(body)
	if err == nil {
		t.Fatal("expected an error when the API returns one")
	}
	for _, want := range []string{"no such problem", "not-found", "404"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not surface %q from the API error object", err, want)
		}
	}
}

func TestParseTemplateResponseRejectsEmptyContent(t *testing.T) {
	body := []byte(`{"result":{"content":""},"error":null}`)
	if _, err := ParseTemplateResponse(body); err == nil {
		t.Fatal("expected an error when the template has no content: a solution needs it")
	}
}
