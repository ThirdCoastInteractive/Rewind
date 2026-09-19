package agent

import "testing"

func TestRunStateTransitions(t *testing.T) {
	for _, pair := range [][2]string{{"running", "waiting_approval"}, {"waiting_approval", "running"}, {"waiting_input", "cancelled"}, {"running", "interrupted"}} {
		if err := ValidateTransition(pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
	}
	for _, pair := range [][2]string{{"completed", "running"}, {"interrupted", "running"}, {"cancelled", "queued"}, {"waiting_approval", "completed"}, {"running", "queued"}} {
		if ValidateTransition(pair[0], pair[1]) == nil {
			t.Fatalf("accepted unsafe transition %v", pair)
		}
	}
}
