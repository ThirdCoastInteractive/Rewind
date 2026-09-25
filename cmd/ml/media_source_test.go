package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPrivateMasterURLSignsWorkerRequest(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	got, err := privateMasterURL("org/acme/master.mp4", "https://media.example.test", "secret", now)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "https" || u.Host != "media.example.test" || u.Path != "/org/acme/master.mp4" {
		t.Fatalf("unexpected signed URL: %s", got)
	}
	exp, _ := strconv.ParseInt(u.Query().Get("exp"), 10, 64)
	if exp != now.Add(privateMasterTTL).Unix() {
		t.Fatalf("expiry=%d", exp)
	}
	mac := hmac.New(sha256.New, []byte("secret"))
	_, _ = mac.Write([]byte("GET\norg/acme/master.mp4\n" + strconv.FormatInt(exp, 10)))
	if want := hex.EncodeToString(mac.Sum(nil)); u.Query().Get("sig") != want {
		t.Fatalf("signature mismatch")
	}
}

func TestPrivateMasterURLRejectsUntrustedKeysAndBases(t *testing.T) {
	tests := []struct{ name, key, base string }{
		{"absolute key", "https://evil.example/x", "https://media.example.test"},
		{"traversal", "org/acme/../master.mp4", "https://media.example.test"},
		{"encoded", "org/acme/master%2emp4", "https://media.example.test"},
		{"http base", "org/acme/master.mp4", "http://media.example.test"},
		{"foreign port", "org/acme/master.mp4", "https://media.example.test:8443"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := privateMasterURL(tt.key, tt.base, "secret", time.Now()); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestPrivateMasterURLExpiryIsBounded(t *testing.T) {
	now := time.Now()
	got, err := privateMasterURL("org/acme/master.mp4", "https://media.example.test", "secret", now)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(got)
	exp, _ := strconv.ParseInt(u.Query().Get("exp"), 10, 64)
	if exp <= now.Unix() || exp > now.Add(privateMasterTTL).Unix() {
		t.Fatalf("unbounded expiry: %d", exp)
	}
	if strings.Contains(got, "secret") {
		t.Fatal("secret leaked into URL")
	}
}
