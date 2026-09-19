//go:build releaseaudit

package agent

import (
	"strings"
	"testing"
	"thirdcoast.systems/rewind/internal/modelruntime"
)

func TestFollowupRetainsPriorConversationWhenItFits(t *testing.T) {
	messages := []modelruntime.Message{
		{Role: "system", Content: "Use archive evidence."},
		{Role: "user", Content: "Find the clip about Bryan's Tesla."},
		{Role: "assistant", Content: "Found it: /videos/123?t=45"},
		{Role: "user", Content: "Make that one ten seconds longer."},
	}
	bounded, err := BoundContext(messages, 8192, false)
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for _, m := range bounded {
		text += m.Content
	}
	if !strings.Contains(text, "Tesla") || !strings.Contains(text, "/videos/123") {
		t.Fatalf("follow-up lost the prior request and selected artifact despite ample context: %#v", bounded)
	}
}
