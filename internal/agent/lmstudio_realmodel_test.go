//go:build realmodel

package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"thirdcoast.systems/rewind/internal/modelruntime"
)

// This evaluation-only transport keeps the real Rewind coordinator and tools.
// LM Studio does not expose a weight digest through the compatibility endpoint;
// the identity below is explicitly a provider reference, not a verified digest.
func serveLMStudioEvaluation(w http.ResponseWriter, r *http.Request, model string) {
	if r.URL.Path == "/v1/model" {
		_ = json.NewEncoder(w).Encode(modelruntime.ModelInfo{Digest: "unverified-lmstudio-reference:" + model, Capabilities: []string{"tools"}})
		return
	}
	var in modelruntime.ChatRequest
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		http.Error(w, "invalid evaluation request", 400)
		return
	}
	var messages []map[string]any
	var pending []string
	for i, message := range in.Messages {
		item := map[string]any{"role": message.Role, "content": message.Content}
		if len(message.ToolCalls) > 0 {
			calls := []map[string]any{}
			pending = nil
			for j, call := range message.ToolCalls {
				id := fmt.Sprintf("call_%d_%d", i, j)
				pending = append(pending, id)
				args, _ := json.Marshal(call.Function.Arguments)
				calls = append(calls, map[string]any{"id": id, "type": "function", "function": map[string]any{"name": call.Function.Name, "arguments": string(args)}})
			}
			item["tool_calls"] = calls
		}
		if message.Role == "tool" {
			if len(pending) == 0 {
				http.Error(w, "orphan tool response", 400)
				return
			}
			item["tool_call_id"] = pending[0]
			pending = pending[1:]
		}
		messages = append(messages, item)
	}
	data, _ := json.Marshal(map[string]any{"model": model, "messages": messages, "tools": in.Tools, "stream": true, "temperature": in.Options["temperature"], "max_tokens": in.Options["num_predict"]})
	req, err := http.NewRequestWithContext(r.Context(), "POST", "http://127.0.0.1:1234/v1/chat/completions", bytes.NewReader(data))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, err.Error(), 503)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		http.Error(w, fmt.Sprintf("LM Studio HTTP %d: %s", response.StatusCode, body), 503)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	encoder := json.NewEncoder(flushWriter{w})
	type assembled struct{ name, arguments string }
	calls := map[int]*assembled{}
	scan := bufio.NewScanner(response.Body)
	scan.Buffer(make([]byte, 4096), 4<<20)
	for scan.Scan() {
		line := scan.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		line = strings.TrimPrefix(line, "data: ")
		if line == "[DONE]" {
			return
		}
		var event struct {
			Choices []struct {
				Delta struct {
					Content   string
					ToolCalls []struct {
						Index    int
						Function struct{ Name, Arguments string }
					} `json:"tool_calls"`
				}
				FinishReason *string `json:"finish_reason"`
			}
		}
		if json.Unmarshal([]byte(line), &event) != nil {
			return
		}
		for _, choice := range event.Choices {
			for _, call := range choice.Delta.ToolCalls {
				if calls[call.Index] == nil {
					calls[call.Index] = &assembled{}
				}
				calls[call.Index].name += call.Function.Name
				calls[call.Index].arguments += call.Function.Arguments
			}
			chunk := modelruntime.Chunk{Message: modelruntime.Message{Role: "assistant", Content: choice.Delta.Content}}
			if choice.FinishReason != nil {
				if *choice.FinishReason == "length" {
					return
				} // Fail incomplete generations; never invent a successful finish.
				chunk.Done = true
				for i := 0; i < len(calls); i++ {
					call := calls[i]
					if call == nil {
						return
					}
					var output modelruntime.ToolCall
					output.Function.Name = call.name
					if json.Unmarshal([]byte(call.arguments), &output.Function.Arguments) != nil {
						return
					}
					chunk.Message.ToolCalls = append(chunk.Message.ToolCalls, output)
				}
			}
			if encoder.Encode(chunk) != nil {
				return
			}
		}
	}
}
