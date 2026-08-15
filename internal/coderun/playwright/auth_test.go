package pwclient

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsLoggedInFalseWhenLoginControlPresent(t *testing.T) {
	html := `<html><body><a data-testid="log-in" href="https://passport.yandex.ru/auth">Войти</a></body></html>`
	if IsLoggedIn(html) {
		t.Error("a page carrying the log-in control must not count as authenticated")
	}
}

func TestIsLoggedInTrueWhenLoginControlAbsent(t *testing.T) {
	html := `<html><body><script id="__NEXT_DATA__">{}</script>` +
		`<button aria-label="Меню профиля"></button>` +
		`<div data-testid="problem-title">2. X</div></body></html>`
	if !IsLoggedIn(html) {
		t.Error("a CodeRun page without the log-in control should count as authenticated")
	}
}

func TestIsLoggedInFalseOnEmptyPage(t *testing.T) {
	// An empty page must never be read as a valid session.
	if IsLoggedIn("") {
		t.Error("empty HTML must not count as authenticated")
	}
}

func TestIsLoggedInFalseOnErrorPage(t *testing.T) {
	// A gateway error page lacks the log-in control purely by accident.
	// Absence-only detection would read this as a live session and let the
	// agent march on against a site that is not actually serving us.
	if IsLoggedIn(`<html><body><h1>502 Bad Gateway</h1></body></html>`) {
		t.Error("an error page must not count as authenticated")
	}
}

func TestIsChallengeUsesSentinel(t *testing.T) {
	// Goto wraps ErrChallenge with %w. Matching on the sentinel rather than on
	// message text means rewording the message cannot silently change
	// behaviour.
	wrapped := fmt.Errorf("navigate to /selections: %w", ErrChallenge)
	if !isChallenge(wrapped) {
		t.Error("isChallenge must recognise a wrapped ErrChallenge")
	}
	if isChallenge(errors.New("coderun presented a challenge")) {
		t.Error("isChallenge must not match on message text alone")
	}
	if isChallenge(nil) {
		t.Error("isChallenge(nil) must be false")
	}
}
