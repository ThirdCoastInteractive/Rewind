package diarize

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiarizeJSONContract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/diarize" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatal("missing bearer")
		}
		var in struct {
			AudioPath string `json:"audio_path"`
			Model     string `json:"model"`
			Device    string `json:"device"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Fatal(err)
		}
		if in.AudioPath != `C:\temp\a.wav` || in.Model != ModelNemotron3 || in.Device != "cuda" {
			t.Fatalf("unexpected request %#v", in)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"fingerprint": "abc",
			"model":       ModelNemotron3,
			"turns": []map[string]any{
				{"start": 0.0, "end": 1.5, "speaker": "speaker_0"},
				{"start": 1.0, "end": 2.0, "speaker": "speaker_1"},
			},
		})
	}))
	defer srv.Close()

	c := &Client{URL: srv.URL, Token: "secret", HTTP: srv.Client()}
	turns, fingerprint, err := c.Diarize(context.Background(), `C:\temp\a.wav`, "", "cuda")
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint != "abc" || len(turns) != 2 || turns[1].Speaker != "speaker_1" {
		t.Fatalf("unexpected response %#v %s", turns, fingerprint)
	}
}

func TestValidateTurnsRejectsNinthSpeaker(t *testing.T) {
	if err := ValidateTurns([]Turn{{Start: 0, End: 1, Speaker: "speaker_8"}}); err == nil {
		t.Fatal("accepted speaker_8")
	}
	if err := ValidateTurns([]Turn{{Start: 1, End: 1, Speaker: "speaker_0"}}); err == nil {
		t.Fatal("accepted zero-length turn")
	}
	if err := ValidateTurns([]Turn{{Start: 0, End: 1, Speaker: "speaker_0"}}); err != nil {
		t.Fatal(err)
	}
}

func TestReady(t *testing.T) {
	if _, ok := Ready([]Model{{Name: ModelNemotron3, Ready: false}}, ""); ok {
		t.Fatal("expected not ready")
	}
	fp, ok := Ready([]Model{{Name: ModelNemotron3, Ready: true, Fingerprint: "fp"}}, "")
	if !ok || fp != "fp" {
		t.Fatalf("ready=%v fp=%s", ok, fp)
	}
}
