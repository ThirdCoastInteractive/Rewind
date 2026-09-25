// Package diarize talks to the local Nemotron diarization sidecar.
// The sidecar is optional. An empty URL or diarize.model=off leaves transcripts unlabeled.
package diarize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	ModelNemotron3 = "nemotron-3"
	PromptVersion  = "nemotron3-offline-v1"
	Priority       = 450
	MaxSpeakers    = 8
)

// Client never downloads weights. Missing weights surface as waiting_model.
type Client struct {
	URL, Token string
	HTTP       *http.Client
}

// Model is one installed diarization recipe.
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
	Models []Model `json:"models"`
}

// Turn is one speaker-active interval. Overlap is two turns, not a merged one.
type Turn struct {
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Speaker string  `json:"speaker"`
}

// FromEnv creates the configured client. An empty URL disables diarization calls.
func FromEnv() *Client {
	return &Client{URL: strings.TrimRight(os.Getenv("DIARIZE_URL"), "/"), Token: os.Getenv("DIARIZE_TOKEN")}
}

func (c *Client) request(ctx context.Context, method, path, contentType string, body io.Reader, out any) error {
	if c.URL == "" {
		return fmt.Errorf("waiting_model: DIARIZE_URL is not configured")
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
		hc = &http.Client{Timeout: 6 * time.Hour}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("diarize HTTP %d: %.1200s", resp.StatusCode, raw)
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

// Install asks the sidecar to download the pinned checkpoint. Diarize itself never does this.
func (c *Client) Install(ctx context.Context, model string) error {
	raw, err := json.Marshal(map[string]string{"model": model, "action": "install"})
	if err != nil {
		return err
	}
	return c.request(ctx, http.MethodPost, "/v1/models/manage", "application/json", bytes.NewReader(raw), &map[string]any{})
}

// Diarize runs the offline profile on a 16 kHz mono WAV the caller just wrote.
func (c *Client) Diarize(ctx context.Context, audioPath, model, device string) ([]Turn, string, error) {
	if model == "" {
		model = ModelNemotron3
	}
	if device != "cuda" {
		device = "cpu"
	}
	raw, err := json.Marshal(map[string]string{
		"audio_path": audioPath,
		"model":      model,
		"device":     device,
	})
	if err != nil {
		return nil, "", err
	}
	var out struct {
		Turns       []Turn `json:"turns"`
		Fingerprint string `json:"fingerprint"`
		Model       string `json:"model"`
	}
	if err = c.request(ctx, http.MethodPost, "/v1/diarize", "application/json", bytes.NewReader(raw), &out); err != nil {
		if strings.Contains(err.Error(), "waiting_model") {
			return nil, "", fmt.Errorf("waiting_model: %w", err)
		}
		return nil, "", err
	}
	if err = ValidateTurns(out.Turns); err != nil {
		return nil, "", err
	}
	return out.Turns, out.Fingerprint, nil
}

// ValidateTurns rejects a ninth speaker and inverted times.
func ValidateTurns(turns []Turn) error {
	for _, turn := range turns {
		if turn.End <= turn.Start {
			return fmt.Errorf("diarize turn has non-positive duration")
		}
		index, ok := speakerIndex(turn.Speaker)
		if !ok || index < 0 || index >= MaxSpeakers {
			return fmt.Errorf("diarize speaker %q is outside speaker_0..speaker_7", turn.Speaker)
		}
	}
	return nil
}

func speakerIndex(speaker string) (int, bool) {
	var index int
	if _, err := fmt.Sscanf(speaker, "speaker_%d", &index); err != nil {
		return 0, false
	}
	if fmt.Sprintf("speaker_%d", index) != speaker {
		return 0, false
	}
	return index, true
}

// Ready reports whether the selected model is installed.
func Ready(models []Model, name string) (string, bool) {
	if name == "" {
		name = ModelNemotron3
	}
	for _, model := range models {
		if model.Name == name && model.Ready && model.Fingerprint != "" {
			return model.Fingerprint, true
		}
	}
	return "", false
}
