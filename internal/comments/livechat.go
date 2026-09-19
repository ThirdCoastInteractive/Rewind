package comments

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// LiveChatSource is the commenters.source for YouTube live-chat replay rows.
// It is distinct from the video's canonical host so the same channel UC is a
// different commenter when they wrote a video comment vs chat.
const LiveChatSource = "youtube.com/live_chat"

// LiveChatToComments converts a yt-dlp live_chat.json payload (NDJSON, array,
// or a single replay object) into yt-dlp-shaped comment objects.
func LiveChatToComments(raw []byte) []map[string]any {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil
	}
	var objs []json.RawMessage
	if raw[0] == '[' {
		if err := json.Unmarshal(raw, &objs); err != nil {
			return nil
		}
	} else {
		for _, line := range bytes.Split(raw, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}
			objs = append(objs, json.RawMessage(line))
		}
	}
	out := make([]map[string]any, 0)
	seen := map[string]bool{}
	for _, obj := range objs {
		for _, c := range extractLiveChatComments(obj) {
			id, _ := c["id"].(string)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, c)
		}
	}
	return out
}

func extractLiveChatComments(raw json.RawMessage) []map[string]any {
	var env map[string]any
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil
	}
	var out []map[string]any
	walkLiveChat(env, &out)
	return out
}

func walkLiveChat(v any, out *[]map[string]any) {
	switch t := v.(type) {
	case map[string]any:
		if c := liveChatComment(t); c != nil {
			*out = append(*out, c)
			return
		}
		for _, child := range t {
			walkLiveChat(child, out)
		}
	case []any:
		for _, child := range t {
			walkLiveChat(child, out)
		}
	}
}

func liveChatComment(m map[string]any) map[string]any {
	renderer, _ := m["liveChatTextMessageRenderer"].(map[string]any)
	if renderer == nil {
		renderer, _ = m["liveChatPaidMessageRenderer"].(map[string]any)
	}
	if renderer == nil {
		return nil
	}
	id, _ := renderer["id"].(string)
	authorID, _ := renderer["authorExternalChannelId"].(string)
	if id == "" || strings.TrimSpace(authorID) == "" {
		return nil
	}
	author := ""
	if name, ok := renderer["authorName"].(map[string]any); ok {
		author, _ = name["simpleText"].(string)
	}
	text := liveChatRunsText(renderer["message"])
	if text == "" {
		text = liveChatRunsText(renderer["headerBackgroundColor"])
	}
	var ts float64
	switch v := renderer["timestampUsec"].(type) {
	case string:
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			ts = n / 1e6
		}
	case float64:
		ts = v / 1e6
	}
	return map[string]any{
		"id":         id,
		"parent":     "root",
		"text":       text,
		"author":     author,
		"author_id":  authorID,
		"author_url": "https://www.youtube.com/channel/" + authorID,
		"timestamp":  ts,
	}
}

func liveChatRunsText(v any) string {
	m, _ := v.(map[string]any)
	if m == nil {
		return ""
	}
	if s, ok := m["simpleText"].(string); ok {
		return s
	}
	runs, _ := m["runs"].([]any)
	var b strings.Builder
	for _, r := range runs {
		rm, _ := r.(map[string]any)
		if rm == nil {
			continue
		}
		if s, ok := rm["text"].(string); ok {
			b.WriteString(s)
		}
	}
	return b.String()
}
