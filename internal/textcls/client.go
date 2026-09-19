// Package textcls talks to the local ONNX comment/speech classifier sidecar.
package textcls

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"thirdcoast.systems/rewind/internal/runtimecfg"
)

const (
	DefaultSentimentModel = "twitter-roberta-sentiment"
	DefaultToxicityModel  = "unbiased-toxic-roberta"
	PromptVersion         = "cls-v1"
	Priority              = 450
	MaxBatch              = 64
)

// Client never downloads models; missing weights surface as waiting_model.
type Client struct {
	URL, Token string
	HTTP       *http.Client
}

// Model describes an installed classifier recipe.
type Model struct {
	Name        string `json:"name"`
	Ready       bool   `json:"ready"`
	Fingerprint string `json:"fingerprint"`
	Revision    string `json:"revision"`
	License     string `json:"license"`
	Recipe      string `json:"recipe"`
	Kind        string `json:"kind"`
	Error       string `json:"error,omitempty"`
}

// ModelsResponse is GET /v1/models.
type ModelsResponse struct {
	Models    []Model  `json:"models"`
	Providers []string `json:"providers"`
	MaxBatch  int      `json:"max_batch"`
}

// Item is one classify request/response row.
type Item struct {
	ID        string             `json:"id"`
	Text      string             `json:"text,omitempty"`
	Sentiment float64            `json:"sentiment"`
	Toxicity  float64            `json:"toxicity"`
	Labels    map[string]any     `json:"labels"`
}

// FromEnv creates the configured service client; empty URL disables background scoring.
func FromEnv() *Client {
	return &Client{URL: strings.TrimRight(os.Getenv("TEXTCLS_URL"), "/"), Token: os.Getenv("TEXTCLS_TOKEN")}
}

func (c *Client) request(ctx context.Context, method, path, contentType string, body io.Reader, out any) error {
	if c.URL == "" {
		return fmt.Errorf("waiting_model: TEXTCLS_URL is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.URL+path, body)
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 120 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("textcls HTTP %d: %.1200s", resp.StatusCode, raw)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// Health probes GET /health.
func (c *Client) Health(ctx context.Context) error {
	var out map[string]any
	return c.request(ctx, http.MethodGet, "/health", "", nil, &out)
}

// Models returns readiness without downloading weights.
func (c *Client) Models(ctx context.Context) (ModelsResponse, error) {
	var out ModelsResponse
	err := c.request(ctx, http.MethodGet, "/v1/models", "", nil, &out)
	return out, err
}

// Classify scores a bounded batch of texts.
func (c *Client) Classify(ctx context.Context, items []Item, sentimentModel, toxicityModel string) ([]Item, error) {
	if len(items) > MaxBatch {
		return nil, fmt.Errorf("textcls batch exceeds %d", MaxBatch)
	}
	if sentimentModel == "" {
		sentimentModel = runtimecfg.String(ctx, "textcls.sentiment_model")
	}
	if toxicityModel == "" {
		toxicityModel = runtimecfg.String(ctx, "textcls.toxicity_model")
	}
	if sentimentModel == "" {
		sentimentModel = DefaultSentimentModel
	}
	if toxicityModel == "" {
		toxicityModel = DefaultToxicityModel
	}
	payload := map[string]any{
		"items":           items,
		"sentiment_model": sentimentModel,
		"toxicity_model":  toxicityModel,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var out struct {
		Items []Item `json:"items"`
	}
	if err = c.request(ctx, http.MethodPost, "/v1/classify", "application/json", bytes.NewReader(raw), &out); err != nil {
		if strings.Contains(err.Error(), "waiting_model") {
			return nil, fmt.Errorf("waiting_model: %w", err)
		}
		return nil, err
	}
	if len(out.Items) != len(items) {
		return nil, fmt.Errorf("textcls returned incomplete batch")
	}
	return out.Items, nil
}

// DigestForSettings builds the ml_jobs model_digest from assigned models and optional fingerprints.
func DigestForSettings(sentiment, toxicity string, models []Model) string {
	if sentiment == "" {
		sentiment = DefaultSentimentModel
	}
	if toxicity == "" {
		toxicity = DefaultToxicityModel
	}
	sentFP, toxFP := "", ""
	for _, m := range models {
		if m.Name == sentiment && m.Ready {
			sentFP = m.Fingerprint
		}
		if m.Name == toxicity && m.Ready {
			toxFP = m.Fingerprint
		}
	}
	sum := sha256.Sum256([]byte(sentiment + "|" + toxicity + "|" + sentFP + "|" + toxFP))
	return hex.EncodeToString(sum[:])
}

// ModelsReady returns true when both assigned classifiers are installed.
func ModelsReady(models []Model, sentiment, toxicity string) bool {
	if sentiment == "" {
		sentiment = DefaultSentimentModel
	}
	if toxicity == "" {
		toxicity = DefaultToxicityModel
	}
	ready := map[string]bool{}
	for _, m := range models {
		ready[m.Name] = m.Ready && m.Fingerprint != ""
	}
	return ready[sentiment] && ready[toxicity]
}
