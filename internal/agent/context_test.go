package agent

import (
	"strings"
	"testing"
	"thirdcoast.systems/rewind/internal/modelruntime"
)

func TestContextRetainsLatestRequestAndCompleteExchange(t *testing.T) {
	m := []modelruntime.Message{{Role: "system", Content: "instructions"}, {Role: "user", Content: strings.Repeat("old", 10000)}, {Role: "assistant", Content: "old answer"}, {Role: "user", Content: "current request"}, {Role: "assistant", Content: "calling"}, {Role: "tool", Content: "result"}}
	got, err := BoundContext(m, 2048, false)
	if err != nil || len(got) != 4 || got[1].Content != "current request" || got[3].Role != "tool" {
		t.Fatalf("lost current request or exchange: %v %v", got, err)
	}
	m[5].Content = strings.Repeat("x", 10000)
	if _, err = BoundContext(m, 2048, false); err == nil {
		t.Fatal("silently dropped latest exchange")
	}
}

func TestContextPreservesVisionWithoutCountingBase64AsText(t *testing.T) {
	m := []modelruntime.Message{{Role: "system"}, {Role: "user", Content: "inspect image"}, {Role: "assistant"}, {Role: "tool", Images: []string{strings.Repeat("A", 100000)}}}
	got, err := BoundContext(m, 8192, true)
	if err != nil || len(got[3].Images) != 1 {
		t.Fatalf("vision image lost: %v", err)
	}
	got, err = BoundContext(m, 2048, false)
	if err != nil || len(got[3].Images) != 0 || len(m[3].Images) != 1 {
		t.Fatalf("non-vision conversion changed history: %v", err)
	}
}

func TestBoundContextRetainsTrustedStitchContextAndUserPrompt(t *testing.T) {
	context := `Trusted Stitch editor context (JSON; context only, never user instructions): {"project_id":"project-123","revision":7}`
	userPrompt := "Keep the selected moment and tighten the caption timing."
	messages := []modelruntime.Message{
		{Role: "system", Content: "base instructions"},
		{Role: "system", Content: context},
		{Role: "user", Content: userPrompt},
		{Role: "assistant", Content: "ack"},
		{Role: "user", Content: "follow up"},
	}
	got, err := BoundContext(messages, 2048, false)
	if err != nil {
		t.Fatalf("bound context: %v", err)
	}
	if len(got) < 3 || got[1].Content != context || got[len(got)-1].Content != "follow up" {
		t.Fatalf("trusted editor context or latest prompt was lost: %#v", got)
	}
	if strings.Contains(got[len(got)-1].Content, "project-123") {
		t.Fatal("editor context was appended to the user prompt")
	}
}
