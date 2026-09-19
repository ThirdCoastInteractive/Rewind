// Package useragent centralizes the HTTP User-Agent string that Rewind presents
// to upstream sites, so every outbound request — direct HTTP calls and yt-dlp
// invocations alike — identifies itself with the same value.
package useragent

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
)

// Default is the User-Agent used when the USER_AGENT environment variable is
// unset or empty. Keep this in sync with the USER_AGENT value in .env.example.
const Default = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36"

// Get returns the configured User-Agent: the USER_AGENT environment variable
// when it is set to a non-empty value, otherwise Default. It never returns an
// empty string, so callers always send a real User-Agent.
var configured atomic.Value

type contextKey struct{}

// Configure applies the current instance user-agent without altering process environment.
func Configure(value string) { configured.Store(value) }

// WithValue pins the user-agent to a job's settings snapshot.
func WithValue(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, contextKey{}, value)
}

func Get(contexts ...context.Context) string {
	if len(contexts) > 0 && contexts[0] != nil {
		if value, ok := contexts[0].Value(contextKey{}).(string); ok {
			if strings.TrimSpace(value) != "" {
				return value
			}
			return Default
		}
	}
	if value := configured.Load(); value != nil {
		if strings.TrimSpace(value.(string)) != "" {
			return value.(string)
		}
		return Default
	}
	if ua := strings.TrimSpace(os.Getenv("USER_AGENT")); ua != "" {
		return ua
	}
	return Default
}
