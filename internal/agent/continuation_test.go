package agent

import (
	"encoding/json"
	"testing"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/modelruntime"
)

func TestContinueRestoresCommittedMutation(t *testing.T) {
	run := &db.AgentRun{Calls: 0, Messages: []byte(`[{"role":"user","content":"Make a project"},{"role":"assistant","tool_calls":[{"function":{"name":"create_stitch_project","arguments":{"plan_id":"owned-plan"}}}]}]`)}
	journal := []*db.AgentToolCall{{CallIndex: 0, Name: "create_stitch_project", Arguments: []byte(`{"plan_id":"owned-plan"}`), Status: "completed", Result: []byte(`{"content":[{"type":"text","text":"existing project"}]}`)}}
	raw, err := ContinuationMessages(run, journal)
	if err != nil {
		t.Fatal(err)
	}
	var messages []modelruntime.Message
	if err = json.Unmarshal(raw, &messages); err != nil {
		t.Fatal(err)
	}
	calls, answered := pendingCalls(messages)
	if len(calls) != answered || messages[len(messages)-1].Content != "existing project\n" {
		t.Fatal("completed mutation would be replayed")
	}
	journal[0].Status = "started"
	if _, err = ContinuationMessages(run, journal); err == nil {
		t.Fatal("uncertain mutation replay allowed")
	}
	journal[0].Status = "completed"
	journal[0].Arguments = []byte(`{"plan_id":"different-plan"}`)
	if _, err = ContinuationMessages(run, journal); err == nil {
		t.Fatal("mismatched journal accepted")
	}
}
