package pwclient

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// loginMarker is the control CodeRun renders only for anonymous visitors.
const loginMarker = `data-testid="log-in"`

// IsLoggedIn reports whether a CodeRun page was rendered for an authenticated
// user. Absence of the log-in control is the signal; an empty page is never
// treated as logged in.
func IsLoggedIn(html string) bool {
	if strings.TrimSpace(html) == "" {
		return false
	}
	return !strings.Contains(html, loginMarker)
}

// AuthStatus loads a cheap CodeRun page and reports session validity.
func (b *Browser) AuthStatus(ctx context.Context) (bool, error) {
	html, err := b.Goto(ctx, "/selections?group=coderun-seasons")
	if err != nil {
		// A challenge here means "not authenticated", which is an answer
		// rather than a failure.
		if isChallenge(err) {
			return false, nil
		}
		return false, err
	}
	return IsLoggedIn(html), nil
}

func isChallenge(err error) bool {
	return err != nil && strings.Contains(err.Error(), ErrChallenge.Error())
}

// AwaitLogin opens the Yandex login page and waits for the operator to finish.
//
// The program never types credentials, never answers a CAPTCHA and never
// handles 2FA. It only watches for the session to become valid.
func (b *Browser) AwaitLogin(ctx context.Context, timeout time.Duration) error {
	const loginURL = "https://passport.yandex.ru/auth?retpath=https%3A%2F%2Fcoderun.yandex.ru%2Fselections"

	if _, err := b.Page.Goto(loginURL); err != nil {
		return fmt.Errorf("open login page: %w", err)
	}

	fmt.Println("A browser window is open. Please log into your Yandex account there.")
	fmt.Println("Complete any 2FA or CAPTCHA yourself — this program will not touch them.")
	fmt.Println("Waiting for the session to become active...")

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}

		ok, err := b.AuthStatus(ctx)
		if err != nil {
			slog.Debug("waiting for login", "error", err)
			continue
		}
		if ok {
			fmt.Println("Authenticated. The session is stored in the browser profile.")
			return nil
		}
	}
	return fmt.Errorf("timed out after %s waiting for login", timeout)
}
