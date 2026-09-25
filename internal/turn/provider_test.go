package turn

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProviderCachesAndRefreshesCredentials(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer private-api-token" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"iceServers":[{"urls":["turn:turn.example:443"],"username":"short-user","credential":"short-password"}]}`)
	}))
	defer server.Close()

	p, err := newProvider(Config{TurnKeyID: "key-id", TurnAPIToken: "private-api-token"}, server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	p.now = func() time.Time { return now }
	p.ttl = time.Hour
	p.refresh = 10 * time.Minute

	first, err := p.Servers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.Servers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("Cloudflare calls after cache hit = %d", calls.Load())
	}
	if first[0].Credential != "short-password" || second[0].Username != "short-user" {
		t.Fatalf("unexpected cached credentials: %#v %#v", first, second)
	}
	if strings.Contains(fmt.Sprintf("%#v", first), "private-api-token") {
		t.Fatal("cached ICE configuration leaked the API token")
	}

	now = now.Add(51 * time.Minute)
	if _, err := p.Servers(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("Cloudflare calls after refresh = %d", calls.Load())
	}
}

func TestProviderExpiryRefreshFailureIsExplicit(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"iceServers":[{"urls":["turn:turn.example:443"],"username":"u","credential":"c"}]}`)
			return
		}
		http.Error(w, "provider unavailable: private-api-token", http.StatusBadGateway)
	}))
	defer server.Close()
	p, err := newProvider(Config{TurnKeyID: "key-id", TurnAPIToken: "private-api-token"}, server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	p.now = func() time.Time { return now }
	p.ttl = time.Hour
	p.refresh = 10 * time.Minute
	if _, err := p.Servers(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(51 * time.Minute)
	_, err = p.Servers(context.Background())
	if err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("refresh error = %v", err)
	}
	if strings.Contains(err.Error(), "private-api-token") {
		t.Fatal("refresh error leaked the API token")
	}
}

func TestProviderRejectsOversizedResponseWithoutCaching(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(strings.Repeat("x", maxResponse+1)))
	}))
	defer server.Close()
	p, err := newProvider(Config{TurnKeyID: "key-id", TurnAPIToken: "private-api-token"}, server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Servers(context.Background())
	if err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("oversized response error = %v", err)
	}
}

func TestProviderUsesStaticServersWhenCloudflareIsAbsent(t *testing.T) {
	p, err := NewProvider(Config{
		STUNURLs:     "stun:one.example, stun:two.example",
		TURNURLs:     "turn:turn.example:443",
		TURNUsername: "static-user",
		TURNPassword: "static-password",
	})
	if err != nil {
		t.Fatal(err)
	}
	servers, err := p.Servers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 2 || servers[1].Credential != "static-password" {
		t.Fatalf("static servers = %#v", servers)
	}
}

func TestProviderRejectsPartialCloudflareConfig(t *testing.T) {
	if _, err := NewProvider(Config{TurnKeyID: "key-id"}); err == nil {
		t.Fatal("expected partial Cloudflare config to fail")
	}
}

func TestFilterTLS443ServersDropsNonTLSAndPreservesCredentials(t *testing.T) {
	servers, err := FilterTLS443Servers([]Server{
		{URLs: []string{"stun:stun.example:3478"}},
		{
			URLs:       []string{"turn:turn.example:3478?transport=udp", "turns:turn.example:443?transport=tcp", "turns:turn.example:5349?transport=tcp"},
			Username:   "short-user",
			Credential: "short-password",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || len(servers[0].URLs) != 1 || servers[0].URLs[0] != "turns:turn.example:443?transport=tcp" {
		t.Fatalf("filtered servers = %#v", servers)
	}
	if servers[0].Username != "short-user" || servers[0].Credential != "short-password" {
		t.Fatalf("credentials not preserved: %#v", servers[0])
	}
}

func TestFilterTLS443ServersFailsClosedWithoutRelay(t *testing.T) {
	_, err := FilterTLS443Servers([]Server{
		{URLs: []string{"stun:stun.example:3478"}},
		{URLs: []string{"turn:turn.example:443?transport=tcp"}},
		{URLs: []string{"turns:turn.example:5349?transport=tcp"}},
	})
	if err == nil || !strings.Contains(err.Error(), "port 443") {
		t.Fatalf("missing TLS 443 relay error = %v", err)
	}
}
