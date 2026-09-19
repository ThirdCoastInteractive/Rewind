package agentprotocol

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestACPStreamsPublicTextAndCancelsPermission(t *testing.T) {
	transcript := `{"id":"rewind-1","result":{"protocolVersion":1,"agentCapabilities":{"mcpCapabilities":{"http":true}}}}
{"id":"rewind-2","result":{"sessionId":"session-one"}}
{"method":"session/update","params":{"sessionId":"session-one","update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"hidden"}}}}
{"method":"session/update","params":{"sessionId":"session-one","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"public"}}}}
{"id":7,"method":"session/request_permission","params":{"sessionId":"session-one","options":[]}}
{"id":"rewind-3","result":{"stopReason":"end_turn"}}
`
	var writes bytes.Buffer
	var text []string
	a := ACP{Peer: NewPeer(context.Background(), strings.NewReader(transcript), &writes, nil), Hooks: Hooks{Event: func(kind string, v any) error {
		if kind == "text" {
			text = append(text, v.(map[string]string)["text"])
		}
		return nil
	}}}
	if err := a.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.Open(context.Background(), "/isolated", "http://rewind/mcp", "scoped", " "); err == nil {
		t.Fatal("unsupported resume accepted")
	}
	if err := a.Open(context.Background(), "/isolated", "http://rewind/mcp", "scoped", ""); err != nil {
		t.Fatal(err)
	}
	if err := a.Prompt(context.Background(), "find clips"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(text, "") != "public" {
		t.Fatalf("hidden content exposed: %v", text)
	}
	if !strings.Contains(writes.String(), `"outcome":"cancelled"`) {
		t.Fatal("permission implicitly approved")
	}
}

func TestCodexRequiresTerminalTurnAndMatchingIdentity(t *testing.T) {
	for _, outcome := range []string{"completed", "failed"} {
		t.Run(outcome, func(t *testing.T) {
			transcript := `{"id":"rewind-1","result":{}}
{"id":"rewind-2","result":{"thread":{"id":"thread-one"}}}
{"id":"rewind-3","result":{"turn":{"id":"turn-one"}}}
{"method":"item/agentMessage/delta","params":{"threadId":"thread-one","turnId":"turn-one","delta":"done"}}
{"method":"turn/completed","params":{"threadId":"thread-one","turn":{"id":"turn-one","status":"` + outcome + `"}}}
`
			var out bytes.Buffer
			c := Codex{Peer: NewPeer(context.Background(), strings.NewReader(transcript), &out, nil)}
			if err := c.Initialize(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := c.Open(context.Background(), "/isolated", "", "http://rewind/mcp", "scoped", ""); err != nil {
				t.Fatal(err)
			}
			err := c.Prompt(context.Background(), "find clips")
			if (err == nil) != (outcome == "completed") {
				t.Fatalf("invalid completion: %v", err)
			}
		})
	}
}

func TestPeerRejectsMismatchedResponse(t *testing.T) {
	p := NewPeer(context.Background(), strings.NewReader("{\"id\":\"wrong\",\"result\":{}}\n"), &bytes.Buffer{}, nil)
	var result json.RawMessage
	if p.Call(context.Background(), "initialize", nil, &result) == nil {
		t.Fatal("accepted wrong response")
	}
}
