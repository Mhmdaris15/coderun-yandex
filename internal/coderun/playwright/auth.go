package pwclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// loginMarker is the control CodeRun renders only for anonymous visitors.
const loginMarker = `data-testid="log-in"`

// IsLoggedIn reports whether a CodeRun page was rendered for an authenticated
// user.
//
// Absence of the log-in control is the signal, but absence alone is not
// enough: a gateway error page, a redirect stub or an empty body all lack that
// control purely by accident and would otherwise read as a valid session. So
// the page must first be recognisable as a CodeRun page at all. The Next.js
// data blob is the cheapest positive marker that is not locale-dependent.
func IsLoggedIn(html string) bool {
	if !strings.Contains(html, "__NEXT_DATA__") {
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

// isChallenge uses the sentinel rather than matching error text. Goto wraps
// ErrChallenge with %w precisely so this works; string matching would break
// silently the moment the sentinel's message is reworded.
func isChallenge(err error) bool {
	return errors.Is(err, ErrChallenge)
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
