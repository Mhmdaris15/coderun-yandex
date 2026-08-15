package pwclient

import "testing"

func TestIsLoggedInFalseWhenLoginControlPresent(t *testing.T) {
	html := `<html><body><a data-testid="log-in" href="https://passport.yandex.ru/auth">Войти</a></body></html>`
	if IsLoggedIn(html) {
		t.Error("a page carrying the log-in control must not count as authenticated")
	}
}

func TestIsLoggedInTrueWhenLoginControlAbsent(t *testing.T) {
	html := `<html><body><button aria-label="Меню профиля"></button><div data-testid="problem-title">2. X</div></body></html>`
	if !IsLoggedIn(html) {
		t.Error("a page without the log-in control should count as authenticated")
	}
}

func TestIsLoggedInFalseOnEmptyPage(t *testing.T) {
	// An empty or error page must never be read as a valid session.
	if IsLoggedIn("") {
		t.Error("empty HTML must not count as authenticated")
	}
}
