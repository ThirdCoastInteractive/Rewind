package mlcore

import (
	"strings"
	"testing"
)

func TestParseWindowsJSONLocalRepair(t *testing.T) {
	raw := "```json\n{\"windows\": [{\"title\":\"A\",\"summary\":\"s\",\"topics\":[],\"entities\":[],\"cue_start\":1,\"cue_end\":2,}]}\n```"
	wins, err := ParseWindowsJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(wins) != 1 || wins[0].Title != "A" || wins[0].CueStart != 1 {
		t.Fatalf("%+v", wins)
	}
}

func TestDecodeWindowsRepairOnce(t *testing.T) {
	calls := 0
	valid := `{"windows":[{"title":"OK","summary":"s","topics":[],"entities":[],"cue_start":1,"cue_end":1}]}`
	wins, err := DecodeWindows("NOT JSON {", func(prev string) (string, error) {
		calls++
		if !strings.Contains(prev, "NOT JSON") {
			t.Fatalf("repair payload: %q", prev)
		}
		return valid, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("repair should run once, got %d", calls)
	}
	if len(wins) != 1 || wins[0].Title != "OK" {
		t.Fatalf("%+v", wins)
	}

	calls = 0
	_, err = DecodeWindows("STILL BAD", func(string) (string, error) {
		calls++
		return "also-not-json", nil
	})
	if err == nil {
		t.Fatal("expected parse error after one failed repair")
	}
	if calls != 1 {
		t.Fatalf("must not repair twice, got %d", calls)
	}
}

func TestParseWindowsJSONNestedShorts(t *testing.T) {
	raw := `{"windows":[{"title":"Chapter","summary":"talk","topics":[],"entities":[],"cue_start":1,"cue_end":20,"shorts":[{"title":"Punch","hook":"that's the joke","cue_start":8,"cue_end":11}]}]}`
	wins, err := ParseWindowsJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(wins) != 1 || len(wins[0].Shorts) != 1 || wins[0].Shorts[0].Title != "Punch" || wins[0].Shorts[0].Hook != "that's the joke" {
		t.Fatalf("%+v", wins)
	}
}

func TestSystemPromptDurableTopics(t *testing.T) {
	p := SystemPrompt()
	for _, need := range []string{"durable subjects", "known-topics", "procedural", "entities are people"} {
		if !strings.Contains(p, need) {
			t.Fatalf("system prompt missing %q", need)
		}
	}
	if PromptVersion != "context-v5-topics" {
		t.Fatalf("PromptVersion %q", PromptVersion)
	}
}

func TestDecodeWindowsNoRepairWhenValid(t *testing.T) {
	raw := `{"windows":[{"title":"T","summary":"","topics":[],"entities":[],"cue_start":1,"cue_end":2}]}`
	calls := 0
	if _, err := DecodeWindows(raw, func(string) (string, error) {
		calls++
		return "", nil
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("repair called on valid JSON")
	}
}
