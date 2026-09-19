// Package vision connects timestamped Rewind media to a private inference service.
package vision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"thirdcoast.systems/rewind/internal/runtimecfg"
	"time"
)

// CLIPModel is the pinned visual/text embedding family.
const CLIPModel = "ViT-B-32__openai"

// Client never downloads models or sends original media files to inference.
type Client struct {
	URL, Token string
	HTTP       *http.Client
}

// Model describes an installed, immutable inference recipe.
type Model struct {
	Name        string `json:"name"`
	Ready       bool   `json:"ready"`
	Fingerprint string `json:"fingerprint"`
	Dimensions  int    `json:"dimensions"`
	Recipe      string `json:"recipe"`
	Revision    string `json:"revision"`
	License     string `json:"license"`
}

// Capabilities describes available models and providers.
type Capabilities struct {
	Models    []Model  `json:"models"`
	Providers []string `json:"providers"`
}

// Prediction is compatible with the CLIP response.
type Prediction struct {
	CLIP   string `json:"clip"`
	Width  int    `json:"imageWidth"`
	Height int    `json:"imageHeight"`
}

// BatchItem preserves the caller's identifier even when an individual image fails.
type BatchItem struct {
	ID     string          `json:"id"`
	Result Prediction      `json:"result"`
	Error  json.RawMessage `json:"error,omitempty"`
}

// FromEnv creates the configured service client; an empty URL disables background indexing.
func FromEnv() *Client {
	return &Client{URL: strings.TrimRight(os.Getenv("VISION_URL"), "/"), Token: os.Getenv("VISION_TOKEN")}
}
func (c *Client) request(ctx context.Context, path, contentType string, body io.Reader, out any) error {
	if c.URL == "" {
		return fmt.Errorf("waiting_model: VISION_URL is not configured")
	}
	method := http.MethodPost
	if body == nil {
		method = http.MethodGet
	}
	req, e := http.NewRequestWithContext(ctx, method, c.URL+path, body)
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", contentType)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 150 * time.Second}
	}
	resp, e := hc.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if e != nil {
		return e
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("vision HTTP %d: %.1200s", resp.StatusCode, raw)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// Capabilities returns readiness without warming any GPU models.
func (c *Client) Capabilities(ctx context.Context) (Capabilities, error) {
	var out Capabilities
	e := c.request(ctx, "/v1/capabilities", "", nil, &out)
	return out, e
}

// Release drains inference and waits for the GPU child process to exit.
// An unreachable vision host is treated as already released so Ollama work
// can proceed when visual indexing is not deployed.
func (c *Client) Release(ctx context.Context) error {
	if c.URL == "" {
		return nil
	}
	var out struct {
		Released bool `json:"released"`
	}
	if e := c.request(ctx, "/v1/runtime/release", "application/json", strings.NewReader("{}"), &out); e != nil {
		return nil
	}
	if !out.Released {
		return fmt.Errorf("vision GPU release not acknowledged")
	}
	return nil
}
func entries(text bool) string {

	kind := "visual"
	if text {
		kind = "textual"
	}
	return fmt.Sprintf(`{"clip":{"%s":{"modelName":"ViT-B-32__openai"}}}`, kind)
}

// Predict encodes an interactive text or image query on CPU.
func (c *Client) Predict(ctx context.Context, text string, image []byte) (Prediction, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("entries", entries(image == nil))
	if image != nil {
		p, e := w.CreateFormFile("image", "reference.jpg")
		if e != nil {
			return Prediction{}, e
		}
		_, _ = p.Write(image)
	} else {
		_ = w.WriteField("text", text)
	}
	_ = w.Close()
	var out Prediction
	e := c.request(ctx, "/predict", w.FormDataContentType(), &body, &out)
	return out, e
}

// Batch indexes bounded images on the selected inference provider.
func (c *Client) Batch(ctx context.Context, ids []string, images [][]byte) ([]BatchItem, error) {
	if len(ids) < 1 || len(ids) > 32 || len(ids) != len(images) {
		return nil, fmt.Errorf("invalid vision batch")
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("entries", entries(false))
	device := runtimecfg.String(ctx, "vision.device")
	if device == "" {
		device = "cpu"
	}
	_ = w.WriteField("device", device)
	raw, _ := json.Marshal(ids)
	_ = w.WriteField("ids", string(raw))
	for _, img := range images {
		p, e := w.CreateFormFile("images", "frame.jpg")
		if e != nil {
			return nil, e
		}
		_, _ = p.Write(img)
	}
	_ = w.Close()
	if body.Len() > 24<<20 {
		return nil, fmt.Errorf("vision batch exceeds byte limit")
	}
	var out struct {
		Results []BatchItem `json:"results"`
	}
	e := c.request(ctx, "/v1/predict-batch", w.FormDataContentType(), &body, &out)
	if e != nil {
		return nil, e
	}
	if len(out.Results) != len(ids) {
		return nil, fmt.Errorf("vision returned incomplete batch")
	}
	for i, r := range out.Results {
		if r.ID != ids[i] {
			return nil, fmt.Errorf("vision returned mismatched batch IDs")
		}
	}
	return out.Results, nil
}

// Normalize validates an embedding and returns a unit-length pgvector input.
func Normalize(raw string) (string, error) {
	var v []float64
	if e := json.Unmarshal([]byte(raw), &v); e != nil {
		return "", e
	}
	if len(v) != 512 {
		return "", fmt.Errorf("expected 512-dimensional embedding")
	}
	var norm float64
	for _, x := range v {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return "", fmt.Errorf("nonfinite embedding")
		}
		norm += x * x
	}
	if norm <= 0 || math.IsInf(norm, 0) {
		return "", fmt.Errorf("invalid embedding norm")
	}
	norm = math.Sqrt(norm)
	for i := range v {
		v[i] /= norm
	}
	b, e := json.Marshal(v)
	return string(b), e
}
