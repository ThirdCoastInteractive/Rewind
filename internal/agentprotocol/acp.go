package agentprotocol

import (
	"context"
	"encoding/json"
	"fmt"
)

// Hooks persist normalized public updates and correlate owned permission responses.
type Hooks struct {
	Event      func(string, any) error
	Permission func(context.Context, string, json.RawMessage) (any, error)
}

// ACP implements the Grok 1.0.13 ACP v1 session contract.
type ACP struct {
	Peer        *Peer
	Hooks       Hooks
	SessionID   string
	LoadSession bool
	HTTPMCP     bool
}

// Initialize checks protocol and transport capabilities without loading a model or issuing a prompt.
func (a *ACP) Initialize(ctx context.Context) error {
	a.Peer.Handle = a.handle
	var result struct {
		ProtocolVersion   int `json:"protocolVersion"`
		AgentCapabilities struct {
			LoadSession bool `json:"loadSession"`
			MCP         struct {
				HTTP bool `json:"http"`
			} `json:"mcpCapabilities"`
		} `json:"agentCapabilities"`
	}
	if err := a.Peer.Call(ctx, "initialize", map[string]any{"protocolVersion": 1, "clientInfo": map[string]string{"name": "rewind", "version": "1.0.0"}, "clientCapabilities": map[string]any{"fs": map[string]bool{"readTextFile": false, "writeTextFile": false}, "terminal": false}}, &result); err != nil {
		return err
	}
	if result.ProtocolVersion != 1 {
		return fmt.Errorf("unsupported ACP version %d", result.ProtocolVersion)
	}
	a.LoadSession = result.AgentCapabilities.LoadSession
	a.HTTPMCP = result.AgentCapabilities.MCP.HTTP
	return nil
}

// Authenticate selects Grok's documented headless API-key authentication method.
func (a *ACP) Authenticate(ctx context.Context) error {
	return a.Peer.Call(ctx, "authenticate", map[string]any{"methodId": "xai.api_key", "_meta": map[string]bool{"headless": true}}, nil)
}

// Open creates or loads a session using only the scoped Rewind MCP endpoint.
func (a *ACP) Open(ctx context.Context, cwd, endpoint, token, resume string) error {
	if !a.HTTPMCP {
		return fmt.Errorf("configured ACP agent does not support HTTP MCP")
	}
	params := map[string]any{"cwd": cwd, "mcpServers": []any{map[string]any{"type": "http", "name": "rewind", "url": endpoint, "headers": []any{map[string]string{"name": "Authorization", "value": "Bearer " + token}}}}}
	if resume != "" {
		if !a.LoadSession {
			return fmt.Errorf("ACP session resume is unsupported")
		}
		a.SessionID = resume
		params["sessionId"] = resume
		return a.Peer.Call(ctx, "session/load", params, nil)
	}
	var result struct {
		SessionID string `json:"sessionId"`
	}
	if err := a.Peer.Call(ctx, "session/new", params, &result); err != nil {
		return err
	}
	if result.SessionID == "" {
		return fmt.Errorf("ACP returned an empty session identity")
	}
	a.SessionID = result.SessionID
	if a.Hooks.Event != nil {
		return a.Hooks.Event("session", map[string]string{"session_id": a.SessionID})
	}
	return nil
}

// Prompt receives text through updates and validates the final stop reason.
func (a *ACP) Prompt(ctx context.Context, text string) error {
	if a.SessionID == "" {
		return fmt.Errorf("ACP session is not open")
	}
	var result struct {
		StopReason string `json:"stopReason"`
	}
	if err := a.Peer.Call(ctx, "session/prompt", map[string]any{"sessionId": a.SessionID, "prompt": []any{map[string]string{"type": "text", "text": text}}}, &result); err != nil {
		return err
	}
	if result.StopReason != "end_turn" {
		return fmt.Errorf("ACP turn stopped: %s", result.StopReason)
	}
	return nil
}

// Cancel requests cancellation without creating another prompt or session.
func (a *ACP) Cancel() error {
	return a.Peer.Notify("session/cancel", map[string]string{"sessionId": a.SessionID})
}

func (a *ACP) handle(ctx context.Context, p Packet) (any, error) {
	var params struct {
		SessionID string `json:"sessionId"`
		Update    struct {
			SessionUpdate string `json:"sessionUpdate"`
			Content       struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			ToolCallID string `json:"toolCallId"`
			Title      string `json:"title"`
			Status     string `json:"status"`
		} `json:"update"`
	}
	if err := json.Unmarshal(p.Params, &params); err != nil {
		return nil, err
	}
	if a.SessionID != "" && params.SessionID != a.SessionID {
		return nil, fmt.Errorf("ACP session identity mismatch")
	}
	if p.Method == "session/request_permission" {
		if a.Hooks.Permission == nil {
			return map[string]any{"outcome": map[string]string{"outcome": "cancelled"}}, nil
		}
		return a.Hooks.Permission(ctx, string(p.ID), p.Params)
	}
	if p.Method != "session/update" {
		if len(p.ID) > 0 {
			return nil, &RPCError{Code: -32601, Message: "client filesystem and terminal operations are disabled"}
		}
		return nil, nil
	}
	if a.Hooks.Event == nil {
		return nil, nil
	}
	switch params.Update.SessionUpdate {
	case "agent_message_chunk":
		if params.Update.Content.Type == "text" {
			return nil, a.Hooks.Event("text", map[string]string{"text": params.Update.Content.Text})
		}
	case "tool_call", "tool_call_update":
		return nil, a.Hooks.Event("tool", map[string]string{"id": params.Update.ToolCallID, "name": params.Update.Title, "status": params.Update.Status})
	}
	// Reasoning, raw tool payloads, and unknown extension fields are deliberately excluded.
	return nil, nil
}
