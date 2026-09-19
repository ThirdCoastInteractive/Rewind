package agent

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"strings"

	"thirdcoast.systems/rewind/internal/modelruntime"
)

// BoundContext keeps the latest user request and complete recent tool exchanges.
// Image bytes are transport data, not text tokens; reserve 4096 tokens per image.
// The full history remains in PostgreSQL. Never silently discard the latest result.
func BoundContext(messages []modelruntime.Message, tokens int, vision bool) ([]modelruntime.Message, error) {
	if len(messages) < 2 || tokens <= 0 {
		return nil, fmt.Errorf("missing request or insufficient context budget")
	}
	request := -1
	for i := range messages {
		if messages[i].Role == "user" {
			request = i
		}
	}
	if request < 1 {
		return nil, fmt.Errorf("missing user request")
	}
	copyMessage := func(m modelruntime.Message) modelruntime.Message {
		if !vision {
			m.Images = nil
		}
		return m
	}
	cost := func(group []modelruntime.Message) int {
		n := 0
		for _, m := range group {
			images := len(m.Images)
			m.Images = nil
			raw, _ := json.Marshal(m)
			n += len(raw)
			if vision {
				n += images * 8192
			}
		}
		return n
	}
	base := []modelruntime.Message{copyMessage(messages[0])}
	// Keep the newest validated Stitch context beside the latest user request,
	// even when the bounded history drops the preceding messages.
	for i := request - 1; i >= 0; i-- {
		if messages[i].Role == "system" && strings.HasPrefix(messages[i].Content, "Trusted Stitch editor context") {
			base = append(base, copyMessage(messages[i]))
			break
		}
	}
	base = append(base, copyMessage(messages[request]))
	if refs := durableReferences(messages); len(refs) > 0 {
		base[0].Content += "\nPersisted compilation references from earlier tool results: " + strings.Join(refs, ", ") + ". Load get_compilation_plan before modifying an earlier plan."
	}
	limit, used := tokens*2, cost(base)+1024
	if used >= limit {
		return nil, fmt.Errorf("request and tool catalogue exceed selected context budget")
	}
	var groups [][]modelruntime.Message
	for _, m := range messages[request+1:] {
		m = copyMessage(m)
		if m.Role != "tool" || len(groups) == 0 {
			groups = append(groups, []modelruntime.Message{m})
		} else {
			groups[len(groups)-1] = append(groups[len(groups)-1], m)
		}
	}
	start := len(groups)
	for i := len(groups) - 1; i >= 0; i-- {
		n := cost(groups[i])
		if used+n > limit {
			if i == len(groups)-1 {
				return nil, fmt.Errorf("latest tool exchange exceeds context budget; use a larger context or smaller tool results")
			}
			break
		}
		used += n
		start = i
	}
	for _, group := range groups[start:] {
		base = append(base, group...)
	}
	// Prefer the current turn, then add whole preceding user turns while they
	// fit. Never orphan an assistant tool call from its results or its request.
	end := request
	for end > 1 {
		begin := end - 1
		for begin > 1 && messages[begin].Role != "user" {
			begin--
		}
		turn := make([]modelruntime.Message, 0, end-begin)
		for _, m := range messages[begin:end] {
			turn = append(turn, copyMessage(m))
		}
		n := cost(turn)
		if used+n > limit {
			break
		}
		used += n
		base = append(append(append([]modelruntime.Message{}, base[0]), turn...), base[1:]...)
		end = begin
	}
	return base, nil
}

func durableReferences(messages []modelruntime.Message) []string {
	seen := map[string]bool{}
	refs := []string{}
	var visit func(any)
	visit = func(value any) {
		if len(refs) >= 16 {
			return
		}
		switch v := value.(type) {
		case map[string]any:
			for _, key := range []string{"plan_id", "project_id"} {
				if text, ok := v[key].(string); ok {
					if id, err := uuid.Parse(text); err == nil {
						ref := key + "=" + id.String()
						if !seen[ref] {
							seen[ref] = true
							refs = append(refs, ref)
						}
					}
				}
			}
			for _, child := range v {
				visit(child)
			}
		case []any:
			for _, child := range v {
				visit(child)
			}
		}
	}
	for i := len(messages) - 1; i >= 0 && len(refs) < 16; i-- {
		if messages[i].Role != "tool" {
			continue
		}
		var value any
		if json.Unmarshal([]byte(messages[i].Content), &value) == nil {
			visit(value)
		}
	}
	return refs
}
