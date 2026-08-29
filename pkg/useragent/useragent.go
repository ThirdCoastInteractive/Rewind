// Package useragent centralizes the HTTP User-Agent string that Rewind presents
// to upstream sites, so every outbound request — direct HTTP calls and yt-dlp
// invocations alike — identifies itself with the same value.
package useragent

import (
	"os"
	"strings"
)

// Default is the User-Agent used when the USER_AGENT environment variable is
// unset or empty. Keep this in sync with the USER_AGENT value in .env.example.
const Default = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36"

// Get returns the configured User-Agent: the USER_AGENT environment variable
// when it is set to a non-empty value, otherwise Default. It never returns an
// empty string, so callers always send a real User-Agent.
func Get() string {
	if ua := strings.TrimSpace(os.Getenv("USER_AGENT")); ua != "" {
		return ua
	}
	return Default
}
