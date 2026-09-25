package templates

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"thirdcoast.systems/rewind/cmd/web/ctxkeys"
	"thirdcoast.systems/rewind/pkg/plugin"
)

func renderAuth(t *testing.T, live bool, siteKey, notice string, page templ.Component) string {
	t.Helper()
	plugin.SetAuthShieldSiteKey(siteKey)
	t.Cleanup(func() { plugin.SetAuthShieldSiteKey("") })
	ctx := context.Background()
	if live {
		ctx = context.WithValue(ctx, ctxkeys.LiveProduct, true)
	}
	if notice != "" {
		ctx = plugin.WithAuthNotice(ctx, notice)
	}
	var buf bytes.Buffer
	if err := page.Render(ctx, &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestLiveAuthShield(t *testing.T) {
	for _, page := range []struct {
		name string
		fn   func(string) templ.Component
	}{
		{"login", Login},
		{"register", Register},
	} {
		t.Run(page.name+" oss", func(t *testing.T) {
			body := renderAuth(t, false, "site-key", "", page.fn(""))
			if strings.Contains(body, `name="website"`) || strings.Contains(body, "cf-turnstile") {
				t.Fatalf("OSS %s rendered the Live auth shield:\n%s", page.name, body)
			}
		})
		t.Run(page.name+" live", func(t *testing.T) {
			body := renderAuth(t, true, "site-key", "The captcha check didn't go through. Please try again.", page.fn(""))
			for _, want := range []string{`name="website"`, "cf-turnstile", "data-sitekey=\"site-key\"", "challenges.cloudflare.com/turnstile", "The captcha check"} {
				if !strings.Contains(body, want) {
					t.Fatalf("Live %s missing %q", page.name, want)
				}
			}
		})
		t.Run(page.name+" live without captcha", func(t *testing.T) {
			body := renderAuth(t, true, "", "", page.fn(""))
			if !strings.Contains(body, `name="website"`) {
				t.Fatalf("Live %s missing honeypot", page.name)
			}
			if strings.Contains(body, "cf-turnstile") {
				t.Fatalf("Live %s rendered Turnstile with an empty site key", page.name)
			}
		})
	}
}
