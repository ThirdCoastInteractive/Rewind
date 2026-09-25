package templates

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"thirdcoast.systems/rewind/cmd/web/ctxkeys"
)

func TestLiveSettingsKeepsAssistantKey(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxkeys.LiveProduct, true)
	var html bytes.Buffer
	if err := SettingsContent("", "", "rw_once", nil).Render(ctx, &html); err != nil {
		t.Fatal(err)
	}
	text := html.String()
	for _, need := range []string{`action="/settings/tokens"`, "rw_once", "assistant to this workspace"} {
		if !strings.Contains(text, need) {
			t.Fatalf("live settings missing %q", need)
		}
	}
	for _, hidden := range []string{"cookies.txt", "BOOKMARKLET", "follow, and download", "Archive Video"} {
		if strings.Contains(text, hidden) {
			t.Fatalf("live settings showed downloader UI %q", hidden)
		}
	}
}

func TestOSSSettingsKeepsDownloaderAndToken(t *testing.T) {
	var html bytes.Buffer
	if err := SettingsContent("", "", "", nil).Render(context.Background(), &html); err != nil {
		t.Fatal(err)
	}
	text := html.String()
	for _, need := range []string{"cookies.txt", "BOOKMARKLET", `action="/settings/tokens"`, "follow, and download"} {
		if !strings.Contains(text, need) {
			t.Fatalf("oss settings missing %q", need)
		}
	}
}
