package plugin

import (
	"context"
	"strings"
)

// Auth shield is the optional public-form captcha configured by Rewind Live.
// An empty site key leaves the widget off. OSS never sets it.

var authShieldSiteKey string

// SetAuthShieldSiteKey installs the Turnstile site key rendered on Live auth forms.
func SetAuthShieldSiteKey(key string) {
	mu.Lock()
	authShieldSiteKey = strings.TrimSpace(key)
	mu.Unlock()
}

// AuthShieldSiteKey returns the Turnstile site key, or empty when captcha is off.
func AuthShieldSiteKey() string {
	mu.RLock()
	defer mu.RUnlock()
	return authShieldSiteKey
}

type authNoticeKey struct{}

// WithAuthNotice stores a one-time auth-form message on ctx for the login and
// register templates. The gate sets it before re-rendering, so the handler
// itself does not learn why the previous POST was refused.
func WithAuthNotice(ctx context.Context, msg string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ctx
	}
	return context.WithValue(ctx, authNoticeKey{}, msg)
}

// AuthNotice returns the message stored by WithAuthNotice.
func AuthNotice(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	msg, _ := ctx.Value(authNoticeKey{}).(string)
	return msg
}
