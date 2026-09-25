// Package turn provides bounded, server-side TURN credential generation.
package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	credentialTTL  = 24 * time.Hour
	refreshBefore  = 15 * time.Minute
	requestTimeout = 10 * time.Second
	maxResponse    = 1 << 20
	cloudflareURL  = "https://rtc.live.cloudflare.com/v1/turn/keys/"
)

var errIncompleteCloudflareConfig = errors.New("Cloudflare TURN configuration requires both CF_TURN_KEY_ID and CF_TURN_API_TOKEN")

var errNoTLS443TURN = errors.New("SFU TURN TLS-only mode requires a turns server on port 443")

// Server is an ICE server configuration safe to send to an authorized browser
// or pass to Pion. It never contains the long-lived Cloudflare API token.
type Server struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// Config controls static ICE servers and optional Cloudflare TURN generation.
type Config struct {
	STUNURLs     string
	TURNURLs     string
	TURNUsername string
	TURNPassword string
	TurnKeyID    string
	TurnAPIToken string
}

// Provider returns static ICE servers or cached Cloudflare credentials.
type Provider struct {
	keyID    string
	apiToken string
	endpoint string
	client   *http.Client
	static   []Server
	now      func() time.Time
	ttl      time.Duration
	refresh  time.Duration

	mu        sync.Mutex
	cached    []Server
	expiresAt time.Time
}

// NewProvider validates configuration and creates a lazy credential provider.
// It does not contact Cloudflare until Servers is called.
func NewProvider(cfg Config) (*Provider, error) {
	return newProvider(cfg, http.DefaultClient, cloudflareURL)
}

func newProvider(cfg Config, client *http.Client, endpoint string) (*Provider, error) {
	keyID := strings.TrimSpace(cfg.TurnKeyID)
	token := strings.TrimSpace(cfg.TurnAPIToken)
	if (keyID == "") != (token == "") {
		return nil, errIncompleteCloudflareConfig
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &Provider{
		keyID:    keyID,
		apiToken: token,
		endpoint: strings.TrimRight(endpoint, "/") + "/",
		client:   client,
		static:   staticServers(cfg),
		now:      time.Now,
		ttl:      credentialTTL,
		refresh:  refreshBefore,
	}, nil
}

// Servers returns a copy of the current ICE configuration. Dynamic credentials
// are refreshed before their 24-hour lifetime expires; refresh failures are
// returned to the caller so ICE cannot fail silently.
func (p *Provider) Servers(ctx context.Context) ([]Server, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := p.now()
	if len(p.keyID) == 0 {
		return cloneServers(p.static), nil
	}
	if len(p.cached) > 0 && now.Before(p.expiresAt.Add(-p.refresh)) {
		return cloneServers(p.cached), nil
	}

	servers, err := p.fetch(ctx)
	if err != nil {
		return nil, err
	}
	p.cached = servers
	p.expiresAt = now.Add(p.ttl)
	return cloneServers(p.cached), nil
}

func (p *Provider) fetch(ctx context.Context) ([]Server, error) {
	body, err := json.Marshal(struct {
		TTL int `json:"ttl"`
	}{TTL: int(p.ttl / time.Second)})
	if err != nil {
		return nil, fmt.Errorf("marshal Cloudflare TURN request: %w", err)
	}
	url := p.endpoint + p.keyID + "/credentials/generate-ice-servers"
	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create Cloudflare TURN request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Cloudflare TURN request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return nil, fmt.Errorf("read Cloudflare TURN response: %w", err)
	}
	if len(data) > maxResponse {
		return nil, errors.New("Cloudflare TURN response exceeds size limit")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("Cloudflare TURN request returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		Servers []Server `json:"iceServers"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode Cloudflare TURN response: %w", err)
	}
	if len(result.Servers) == 0 {
		return nil, errors.New("Cloudflare TURN response contained no ICE servers")
	}
	for i := range result.Servers {
		result.Servers[i].URLs = cleanURLs(result.Servers[i].URLs)
		if len(result.Servers[i].URLs) == 0 {
			return nil, errors.New("Cloudflare TURN response contained an empty ICE server")
		}
	}
	return result.Servers, nil
}

func staticServers(cfg Config) []Server {
	var out []Server
	if urls := splitURLs(cfg.STUNURLs); len(urls) > 0 {
		out = append(out, Server{URLs: urls})
	}
	if urls := splitURLs(cfg.TURNURLs); len(urls) > 0 {
		out = append(out, Server{URLs: urls, Username: cfg.TURNUsername, Credential: cfg.TURNPassword})
	}
	return out
}

func splitURLs(raw string) []string {
	var out []string
	for _, value := range strings.Split(raw, ",") {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func cleanURLs(urls []string) []string {
	out := make([]string, 0, len(urls))
	for _, value := range urls {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func cloneServers(servers []Server) []Server {
	out := make([]Server, len(servers))
	for i, server := range servers {
		out[i] = server
		out[i].URLs = append([]string(nil), server.URLs...)
	}
	return out
}

// FilterTLS443Servers returns only TURN-over-TLS URLs on port 443. It is used
// by the SFU transport experiment; browser ICE configuration remains
// unchanged. A caller cannot accidentally enable relay-only mode without a
// usable relay because an empty result is an error.
func FilterTLS443Servers(servers []Server) ([]Server, error) {
	out := make([]Server, 0, len(servers))
	for _, server := range servers {
		filtered := Server{
			Username:   server.Username,
			Credential: server.Credential,
		}
		for _, raw := range server.URLs {
			if isTLS443TURN(raw) {
				filtered.URLs = append(filtered.URLs, strings.TrimSpace(raw))
			}
		}
		if len(filtered.URLs) > 0 {
			out = append(out, filtered)
		}
	}
	if len(out) == 0 {
		return nil, errNoTLS443TURN
	}
	return out, nil
}

func isTLS443TURN(raw string) bool {
	value := strings.TrimSpace(raw)
	parts := strings.SplitN(value, "?", 2)
	base := strings.ToLower(parts[0])
	if !strings.HasPrefix(base, "turns:") {
		return false
	}
	authority := strings.TrimPrefix(base, "turns:")
	authority = strings.TrimPrefix(authority, "//")
	if strings.Contains(authority, "/") || !strings.HasSuffix(authority, ":443") {
		return false
	}
	if len(parts) == 2 {
		query, err := url.ParseQuery(parts[1])
		if err != nil {
			return false
		}
		if transport := strings.ToLower(query.Get("transport")); transport != "" && transport != "tcp" {
			return false
		}
	}
	return authority != ":443"
}
