package agent_api

import (
	"testing"
	"thirdcoast.systems/rewind/internal/db"
)

func TestA2ARejectsUnsupportedOrEmptyMessages(t *testing.T) {
	for _, raw := range []string{`{}`, `{"message":{"role":"ROLE_AGENT"}}`, `{"message":{"role":"ROLE_USER","messageId":"1","parts":[{"file":{"uri":"http://private"}}]}}`, `{"message":{"role":"ROLE_USER","messageId":"1","taskId":"existing","parts":[{"text":"again"}]}}`} {
		if _, _, err := a2aPrompt([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	id, text, err := a2aPrompt([]byte(`{"message":{"role":"ROLE_USER","messageId":"one","parts":[{"text":"Find Adam"},{"text":"in hot tubs"}]}}`))
	if err != nil || id != "one" || text != "Find Adam\nin hot tubs" {
		t.Fatalf("invalid message mapping: %s %s %v", id, text, err)
	}
}

func TestA2ADoesNotClaimCancellationBeforeAcknowledgement(t *testing.T) {
	run := &db.AgentRun{Status: "running", CancelRequested: true}
	got := a2aTask(run)
	if got["status"].(map[string]any)["state"] != "TASK_STATE_WORKING" {
		t.Fatal("cancellation intent reported as completed cancellation")
	}
	run.Status = "cancelled"
	run.CancellationAcknowledged = true
	if a2aTask(run)["status"].(map[string]any)["state"] != "TASK_STATE_CANCELED" {
		t.Fatal("lost cancellation acknowledgement")
	}
}
