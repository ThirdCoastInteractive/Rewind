package agent_api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/agent"
	"thirdcoast.systems/rewind/internal/db"
	rewindmcp "thirdcoast.systems/rewind/internal/mcp"
	"thirdcoast.systems/rewind/internal/modelruntime"
)

type a2aRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// RegisterA2A installs A2A 1.0 JSON-RPC task submission, polling and cancellation.
// Existing MCP read+write bearer scopes authorize durable archive work as that account.
func RegisterA2A(e *echo.Echo, dbc *db.DatabaseConnection) {
	e.POST("/api/a2a", func(c echo.Context) error {
		ctx := c.Request().Context()
		token, err := rewindmcp.Authenticate(ctx, dbc, c.Request().Header.Get("Authorization"))
		if err != nil {
			return echo.NewHTTPError(401)
		}
		read, write := false, false
		for _, scope := range token.Scopes {
			read = read || scope == "mcp:read"
			write = write || scope == "mcp:write"
		}
		if !read {
			return echo.NewHTTPError(403)
		}
		var in a2aRequest
		if json.NewDecoder(http.MaxBytesReader(c.Response(), c.Request().Body, 64<<10)).Decode(&in) != nil || in.JSONRPC != "2.0" || len(in.ID) == 0 {
			return echo.NewHTTPError(400, "invalid JSON-RPC request")
		}
		failure := func(code int, message string) error {
			return c.JSON(200, map[string]any{"jsonrpc": "2.0", "id": in.ID, "error": map[string]any{"code": code, "message": message}})
		}
		if version := c.Request().Header.Get("A2A-Version"); version != "" && version != "1.0" {
			return failure(-32009, "unsupported A2A version; use 1.0")
		}
		var run *db.AgentRun
		switch in.Method {
		case "SendMessage":
			if !write {
				return echo.NewHTTPError(403)
			}
			messageID, prompt, err := a2aPrompt(in.Params)
			if err != nil {
				return failure(-32602, err.Error())
			}
			run, err = agent.SubmitDelegated(ctx, dbc, token.UserID, messageID, prompt)
			if err != nil {
				return failure(-32602, err.Error())
			}
			return c.JSON(200, map[string]any{"jsonrpc": "2.0", "id": in.ID, "result": map[string]any{"task": a2aTask(run)}})
		case "GetTask", "CancelTask":
			var params struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(in.Params, &params) != nil {
				return failure(-32602, "invalid task identity")
			}
			id, err := common.ParseUUID(params.ID)
			if err != nil {
				return failure(-32001, "task not found")
			}
			run, err = dbc.Queries(ctx).GetAgentRun(ctx, &db.GetAgentRunParams{ID: id, UserID: token.UserID})
			if err != nil {
				return failure(-32001, "task not found")
			}
			if in.Method == "CancelTask" {
				if !write {
					return echo.NewHTTPError(403)
				}
				if run.Status == "completed" || run.Status == "failed" || run.Status == "cancelled" || run.Status == "limited" || run.Status == "interrupted" {
					return failure(-32002, "task is already terminal")
				}
				if err = dbc.Queries(ctx).CancelAgentRun(ctx, &db.CancelAgentRunParams{ID: id, UserID: token.UserID}); err != nil {
					return failure(-32603, "could not persist cancellation")
				}
				// Cancellation intent is honest: status remains working until the coordinator acknowledges it.
				run.CancelRequested = true
			}
			return c.JSON(200, map[string]any{"jsonrpc": "2.0", "id": in.ID, "result": a2aTask(run)})
		default:
			return failure(-32601, "unsupported operation")
		}
	})
}

func a2aPrompt(raw json.RawMessage) (string, string, error) {
	var in struct {
		Message struct {
			MessageID string                       `json:"messageId"`
			Role      string                       `json:"role"`
			TaskID    string                       `json:"taskId"`
			ContextID string                       `json:"contextId"`
			Parts     []map[string]json.RawMessage `json:"parts"`
		} `json:"message"`
		Configuration struct {
			Blocking               bool            `json:"blocking"`
			PushNotificationConfig json.RawMessage `json:"pushNotificationConfig"`
		} `json:"configuration"`
	}
	if json.Unmarshal(raw, &in) != nil || in.Message.Role != "ROLE_USER" {
		return "", "", fmt.Errorf("a ROLE_USER message is required")
	}
	if in.Message.TaskID != "" || in.Message.ContextID != "" || in.Configuration.Blocking || len(in.Configuration.PushNotificationConfig) > 0 {
		return "", "", fmt.Errorf("this endpoint accepts asynchronous new tasks; poll GetTask for progress")
	}
	var text []string
	for _, part := range in.Message.Parts {
		var value string
		if len(part) != 1 || json.Unmarshal(part["text"], &value) != nil {
			return "", "", fmt.Errorf("only text parts are supported")
		}
		text = append(text, value)
	}
	prompt := strings.Join(text, "\n")
	if strings.TrimSpace(in.Message.MessageID) == "" || len(in.Message.MessageID) > 200 || strings.TrimSpace(prompt) == "" || len(prompt) > 16000 {
		return "", "", fmt.Errorf("messageId and 1–16000 characters of text are required")
	}
	return in.Message.MessageID, prompt, nil
}

func a2aTask(run *db.AgentRun) map[string]any {
	state := "TASK_STATE_WORKING"
	switch run.Status {
	case "queued":
		state = "TASK_STATE_SUBMITTED"
	case "completed":
		state = "TASK_STATE_COMPLETED"
	case "cancelled":
		state = "TASK_STATE_CANCELED"
	case "waiting_input", "waiting_approval":
		state = "TASK_STATE_INPUT_REQUIRED"
	case "failed", "interrupted", "limited":
		state = "TASK_STATE_FAILED"
	}
	status := map[string]any{"state": state, "timestamp": run.UpdatedAt.Time.UTC().Format(time.RFC3339Nano)}
	var messages []modelruntime.Message
	_ = json.Unmarshal(run.Messages, &messages)
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" && messages[i].Content != "" {
			status["message"] = map[string]any{"role": "ROLE_AGENT", "messageId": run.ID.String() + "-response", "parts": []any{map[string]string{"text": messages[i].Content}}, "taskId": run.ID.String(), "contextId": run.ConversationID.String()}
			break
		}
	}
	var artifacts []map[string]string
	_ = json.Unmarshal(run.Artifacts, &artifacts)
	out := map[string]any{"id": run.ID.String(), "contextId": run.ConversationID.String(), "status": status, "metadata": map[string]any{"rewindStatus": run.Status, "cancellationRequested": run.CancelRequested, "cancellationAcknowledged": run.CancellationAcknowledged, "toolCalls": run.Calls}}
	if len(artifacts) > 0 {
		out["artifacts"] = []any{map[string]any{"artifactId": run.ID.String() + "-artifacts", "name": "Rewind results", "parts": []any{map[string]any{"data": map[string]any{"references": artifacts}}}}}
	}
	return out
}
