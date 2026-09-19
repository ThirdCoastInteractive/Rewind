// Package agent coordinates durable, user-owned assistant runs.
package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"thirdcoast.systems/rewind/internal/db"
)

// Capabilities describes operations implemented and tested by a runtime adapter.
type Capabilities struct {
	Images       bool `json:"images"`
	Tools        bool `json:"tools"`
	Approvals    bool `json:"approvals"`
	Cancellation bool `json:"cancellation"`
	Resume       bool `json:"resume"`
}

// Event is a public update; adapters must exclude hidden reasoning and credentials.
type Event struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

// Runtime lets a provider own its execution loop while Rewind owns persistence.
// External identifiers stay in the run record and must never replace Rewind IDs.
type Runtime interface {
	Name() string
	Capabilities() Capabilities
	Run(context.Context, db.AgentRun, func(Event) error) error
	Cancel(context.Context, db.AgentRun) error
	Answer(context.Context, db.AgentRun, string, json.RawMessage) error
}

// ValidateTransition rejects ambiguous or duplicate execution transitions.
func ValidateTransition(from, to string) error {
	allowed := map[string][]string{
		"queued":           {"running", "cancelled", "failed"},
		"running":          {"waiting_input", "waiting_approval", "waiting_capacity", "completed", "failed", "cancelled", "interrupted", "limited"},
		"waiting_input":    {"running", "cancelled", "interrupted"},
		"waiting_approval": {"running", "cancelled", "interrupted"},
		"waiting_capacity": {"running", "cancelled", "failed", "interrupted", "limited"},
	}
	for _, next := range allowed[from] {
		if to == next {
			return nil
		}
	}
	return fmt.Errorf("invalid agent state transition %q → %q", from, to)
}
