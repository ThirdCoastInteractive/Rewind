package textcls

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClassifyJSONContract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/classify" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		var in struct {
			Items []Item `json:"items"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Fatal(err)
		}
		if len(in.Items) != 1 || in.Items[0].ID != "c1" || in.Items[0].Text != "hello" {
			t.Fatalf("unexpected request %#v", in.Items)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{{
				"id": "c1", "sentiment": 0.25, "toxicity": 0.1,
				"labels": map[string]any{"sentiment": map[string]any{"positive": 0.6}},
			}},
		})
	}))
	defer srv.Close()

	c := &Client{URL: srv.URL, HTTP: srv.Client()}
	out, err := c.Classify(context.Background(), []Item{{ID: "c1", Text: "hello"}}, DefaultSentimentModel, DefaultToxicityModel)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != "c1" || out[0].Sentiment != 0.25 || out[0].Toxicity != 0.1 {
		t.Fatalf("unexpected response %#v", out)
	}
}

func TestDigestAndReady(t *testing.T) {
	models := []Model{
		{Name: DefaultSentimentModel, Ready: true, Fingerprint: "s1"},
		{Name: DefaultToxicityModel, Ready: true, Fingerprint: "t1"},
	}
	if !ModelsReady(models, "", "") {
		t.Fatal("expected ready")
	}
	d1 := DigestForSettings("", "", models)
	d2 := DigestForSettings(DefaultSentimentModel, DefaultToxicityModel, models)
	if d1 == "" || d1 != d2 {
		t.Fatalf("digest mismatch %q %q", d1, d2)
	}
	if ModelsReady([]Model{{Name: DefaultSentimentModel, Ready: false}}, "", "") {
		t.Fatal("expected not ready")
	}
}

func TestHealthAndModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(ModelsResponse{
				Models:   []Model{{Name: DefaultSentimentModel, Ready: false}},
				MaxBatch: 64,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &Client{URL: srv.URL, HTTP: srv.Client()}
	if err := c.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	models, err := c.Models(context.Background())
	if err != nil || models.MaxBatch != 64 || len(models.Models) != 1 {
		t.Fatalf("models=%#v err=%v", models, err)
	}
}
