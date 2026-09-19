package agent

import (
	"encoding/json"
	"fmt"
	"reflect"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/modelruntime"
)

// ContinuationMessages restores completed operations that preceded a lost checkpoint.
// Uncertain mutations must be reconciled before a new run may dispatch more tools.
func ContinuationMessages(run *db.AgentRun, journal []*db.AgentToolCall) ([]byte, error) {
	var messages []modelruntime.Message
	if err := json.Unmarshal(run.Messages, &messages); err != nil {
		return nil, err
	}
	pending, answered := pendingCalls(messages)
	index := run.Calls
	for _, saved := range journal {
		if saved.Status != "completed" {
			return nil, errUncertain
		}
		if saved.CallIndex < index {
			continue
		}
		if saved.CallIndex != index || answered >= len(pending) {
			return nil, fmt.Errorf("journal does not match checkpoint")
		}
		call := pending[answered]
		var args map[string]any
		if json.Unmarshal(saved.Arguments, &args) != nil || saved.Name != call.Function.Name || !reflect.DeepEqual(args, call.Function.Arguments) {
			return nil, errUncertain
		}
		var result *mcpsdk.CallToolResult
		if err := json.Unmarshal(saved.Result, &result); err != nil || result == nil {
			return nil, fmt.Errorf("invalid persisted tool result")
		}
		messages = append(messages, resultMessage(saved.Name, result, true))
		index++
		answered++
	}
	return json.Marshal(messages)
}
