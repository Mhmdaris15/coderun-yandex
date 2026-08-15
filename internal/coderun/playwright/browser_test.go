package pwclient

import (
	"errors"
	"strings"
	"testing"
)

func TestDetectChallengeOnPassportRedirect(t *testing.T) {
	// Being bounced to passport mid-run means the session died. That is a stop
	// condition, never something to retry around.
	err := DetectChallenge("https://passport.yandex.ru/auth?retpath=x", "<html></html>")
	if !errors.Is(err, ErrChallenge) {
		t.Errorf("got %v, want ErrChallenge", err)
	}
}

func TestDetectChallengeOnCaptchaHost(t *testing.T) {
	err := DetectChallenge("https://coderun.yandex.ru/showcaptcha?t=1", "<html></html>")
	if !errors.Is(err, ErrChallenge) {
		t.Errorf("got %v, want ErrChallenge", err)
	}
}

func TestDetectChallengeOnCaptchaMarkup(t *testing.T) {
	html := `<html><body><div class="CheckboxCaptcha">Подтвердите, что запросы отправляли вы</div></body></html>`
	err := DetectChallenge("https://coderun.yandex.ru/selections", html)
	if !errors.Is(err, ErrChallenge) {
		t.Errorf("got %v, want ErrChallenge", err)
	}
}

func TestDetectChallengeAllowsNormalPage(t *testing.T) {
	html := `<html><body><div data-testid="problem-list-item">1. Задача</div></body></html>`
	if err := DetectChallenge("https://coderun.yandex.ru/selections/2025-summer-common", html); err != nil {
		t.Errorf("normal page flagged as a challenge: %v", err)
	}
}

func TestChallengeErrorMentionsURL(t *testing.T) {
	err := DetectChallenge("https://passport.yandex.ru/auth", "")
	if err == nil || !strings.Contains(err.Error(), "passport.yandex.ru") {
		t.Errorf("error should name the URL that triggered it, got %v", err)
	}
}
