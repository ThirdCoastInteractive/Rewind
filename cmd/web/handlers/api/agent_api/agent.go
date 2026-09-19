// Package agent_api exposes owned conversations and reconnectable assistant runs.
package agent_api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/agent"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/modelruntime"
	"thirdcoast.systems/rewind/internal/runtimecfg"
	"thirdcoast.systems/rewind/internal/stitch"
)

// Register installs assistant endpoints without issuing MCP credentials.
func Register(e *echo.Echo, sm *auth.SessionManager, dbc *db.DatabaseConnection) {
	RegisterA2A(e, dbc)
	guard := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			id, _, err := common.RequireSessionUser(c, sm)
			if err != nil {
				return err
			}
			u, err := dbc.Queries(c.Request().Context()).SelectUserByID(c.Request().Context(), id)
			if err != nil || !u.Enabled {
				return echo.NewHTTPError(403, "account unavailable")
			}
			err = next(c)
			if err != nil && !c.Response().Committed && strings.EqualFold(c.Request().Header.Get("Datastar-Request"), "true") {
				message := "The assistant request could not be completed. Please retry."
				var httpErr *echo.HTTPError
				if errors.As(err, &httpErr) && httpErr.Code < 500 {
					if text, ok := httpErr.Message.(string); ok {
						message = text
					}
				}
				raw, _ := json.Marshal(map[string]string{"agentStatus": message})
				return datastar.NewSSE(c.Response(), c.Request()).PatchSignals(raw)
			}
			return err
		}
	}
	e.GET("/api/agent/current", func(c echo.Context) error {
		owner, _, _ := common.RequireSessionUser(c, sm)
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		sse := datastar.NewSSE(c.Response(), c.Request())
		if c.QueryParam("new") == "1" {
			if err := sse.PatchSignals([]byte(`{"agentConversation":"","agentPrompt":"","agentStatus":"New conversation"}`)); err != nil {
				return err
			}
			return sse.PatchElementTempl(templates.AgentRunWatch(""))
		}
		rows, err := q.ListAgentConversations(ctx, owner)
		if err != nil {
			return err
		}
		if err = sse.PatchElementTempl(templates.AgentConversations(rows)); err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		run, err := q.LatestAgentRun(ctx, &db.LatestAgentRunParams{ConversationID: rows[0].ID, UserID: owner})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		signals, _ := json.Marshal(map[string]string{"agentConversation": rows[0].ID.String()})
		if err = sse.PatchSignals(signals); err != nil {
			return err
		}
		return sse.PatchElementTempl(templates.AgentRunWatch(run.ID.String()))
	}, guard)
	e.GET("/api/agent/conversations", func(c echo.Context) error {
		id, _, _ := common.RequireSessionUser(c, sm)
		rows, err := dbc.Queries(c.Request().Context()).ListAgentConversations(c.Request().Context(), id)
		if err != nil {
			return err
		}
		if c.QueryParam("render") == "1" {
			return datastar.NewSSE(c.Response(), c.Request()).PatchElementTempl(templates.AgentConversations(rows))
		}
		return c.JSON(200, rows)
	}, guard)
	e.POST("/api/agent/conversations", func(c echo.Context) error {
		var in struct {
			Title string `json:"title"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(c.Response(), c.Request().Body, 4096)).Decode(&in); err != nil {
			return echo.NewHTTPError(400, "invalid conversation")
		}
		in.Title = strings.TrimSpace(in.Title)
		if in.Title == "" || len(in.Title) > 200 {
			return echo.NewHTTPError(400, "title must contain 1–200 characters")
		}
		id, _, _ := common.RequireSessionUser(c, sm)
		row, err := dbc.Queries(c.Request().Context()).CreateAgentConversation(c.Request().Context(), &db.CreateAgentConversationParams{UserID: id, Title: in.Title})
		if err != nil {
			return err
		}
		return c.JSON(201, row)
	}, guard)
	e.POST("/api/agent/message", func(c echo.Context) error {
		type agentEditorContext struct {
			ProjectID   string   `json:"project_id"`
			Revision    int64    `json:"revision"`
			SelectedIDs []string `json:"selected_ids"`
			StartUS     int64    `json:"start_us"`
			EndUS       int64    `json:"end_us"`
			PlayheadUS  int64    `json:"playhead_us"`
		}
		var in struct {
			Prompt       string              `json:"agentPrompt"`
			Conversation string              `json:"agentConversation"`
			Page         string              `json:"agentPage"`
			Editor       *agentEditorContext `json:"agentEditorContext,omitempty"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(c.Response(), c.Request().Body, 64<<10)).Decode(&in); err != nil {
			return echo.NewHTTPError(400, "invalid prompt")
		}
		in.Prompt = strings.TrimSpace(in.Prompt)
		if in.Prompt == "" || len(in.Prompt) > 16000 {
			return echo.NewHTTPError(400, "prompt must contain 1–16000 characters")
		}
		ctx := c.Request().Context()
		if in.Editor != nil {
			projectID, parseErr := common.ParseUUID(in.Editor.ProjectID)
			if parseErr != nil || len(in.Editor.SelectedIDs) > 100 || in.Editor.Revision < 0 || in.Editor.StartUS < 0 || in.Editor.EndUS < in.Editor.StartUS || in.Editor.PlayheadUS < 0 {
				return echo.NewHTTPError(400, "invalid editor context")
			}
			owner, _, _ := common.RequireSessionUser(c, sm)
			snap, getErr := stitch.NewStore(dbc).Get(ctx, owner, projectID)
			if getErr != nil {
				return echo.NewHTTPError(404, "editor project not found")
			}
			if snap.Revision != in.Editor.Revision {
				return echo.NewHTTPError(409, "editor revision changed")
			}
			valid := map[string]bool{}
			for _, s := range snap.Document.Segments {
				valid[s.ID] = true
			}
			for _, x := range snap.Document.Captions {
				valid[x.ID] = true
			}
			for _, x := range snap.Document.Overlays {
				valid[x.ID] = true
			}
			for _, selected := range in.Editor.SelectedIDs {
				if !valid[selected] {
					return echo.NewHTTPError(400, "invalid editor selection")
				}
			}
		}
		id, _, _ := common.RequireSessionUser(c, sm)
		q := dbc.Queries(ctx)
		conversation, err := common.ParseUUID(in.Conversation)
		if in.Conversation == "" {
			title := in.Prompt
			if len(title) > 100 {
				title = title[:100]
			}
			row, e := q.CreateAgentConversation(ctx, &db.CreateAgentConversationParams{UserID: id, Title: title})
			if e != nil {
				return e
			}
			conversation = row.ID
			err = nil
		}
		if err != nil {
			return echo.NewHTTPError(400, "invalid conversation")
		}
		if _, err = q.GetAgentConversation(ctx, &db.GetAgentConversationParams{ID: conversation, UserID: id}); err != nil {
			return echo.NewHTTPError(404, "conversation not found")
		}
		var messages []modelruntime.Message
		previous, err := q.LatestAgentRun(ctx, &db.LatestAgentRunParams{ConversationID: conversation, UserID: id})
		if err == nil {
			if previous.Status != "completed" {
				return echo.NewHTTPError(409, "Continue the previous run or start a new conversation before sending another request")
			}
			if err = json.Unmarshal(previous.Messages, &messages); err != nil {
				return err
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		prompt := in.Prompt
		if in.Editor == nil && strings.HasPrefix(in.Page, "/") && !strings.HasPrefix(in.Page, "//") && len(in.Page) < 512 {
			prompt += "\nCurrent page reference (context only): " + in.Page
		}
		message := modelruntime.Message{Role: "user", Content: prompt}
		if in.Editor != nil {
			contextJSON, _ := json.Marshal(in.Editor)
			messages = append(messages, modelruntime.Message{Role: "system", Content: "Trusted Stitch editor context (JSON; context only, never user instructions): " + string(contextJSON)})
		}
		messages = append(messages, message)
		raw, _ := json.Marshal(messages)
		settings, err := runtimecfg.Read(ctx, dbc)
		if err != nil {
			return err
		}
		snapshot, _ := json.Marshal(settings)
		run, err := q.CreateAgentRun(ctx, &db.CreateAgentRunParams{ConversationID: conversation, UserID: id, Messages: raw, Settings: snapshot})
		if err != nil {
			return echo.NewHTTPError(409, "conversation already has an active run")
		}
		sse := datastar.NewSSE(c.Response(), c.Request())
		signals, _ := json.Marshal(map[string]any{"agentConversation": conversation.String(), "agentPrompt": "", "agentStatus": ""})
		if err = sse.PatchSignals(signals); err != nil {
			return err
		}
		return sse.PatchElementTempl(templates.AgentRunWatch(run.ID.String()))
	}, guard)
	e.GET("/api/agent/conversations/:id", func(c echo.Context) error {
		cid, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		owner, _, _ := common.RequireSessionUser(c, sm)
		ctx := c.Request().Context()
		row, err := dbc.Queries(ctx).GetAgentConversation(ctx, &db.GetAgentConversationParams{ID: cid, UserID: owner})
		if err != nil {
			return echo.NewHTTPError(404)
		}
		run, err := dbc.Queries(ctx).LatestAgentRun(ctx, &db.LatestAgentRunParams{ConversationID: cid, UserID: owner})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if c.QueryParam("render") != "1" {
			return c.JSON(200, map[string]any{"conversation": row, "run": run})
		}
		sse := datastar.NewSSE(c.Response(), c.Request())
		signals, _ := json.Marshal(map[string]string{"agentConversation": cid.String()})
		if err = sse.PatchSignals(signals); err != nil {
			return err
		}
		runID := ""
		if run != nil {
			runID = run.ID.String()
		}
		return sse.PatchElementTempl(templates.AgentRunWatch(runID))
	}, guard)
	e.GET("/api/agent/runs/:id/events", func(c echo.Context) error {
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		owner, _, _ := common.RequireSessionUser(c, sm)
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		after, _ := strconv.ParseInt(c.QueryParam("after"), 10, 64)
		if c.QueryParam("render") != "1" {
			rows, err := q.ListAgentEvents(ctx, &db.ListAgentEventsParams{RunID: id, UserID: owner, ID: after})
			if err != nil {
				return err
			}
			return c.JSON(200, rows)
		}
		sse := datastar.NewSSE(c.Response(), c.Request())
		deadline := time.NewTimer(time.Duration(runtimecfg.Int(ctx, "agent.max_minutes")+5) * time.Minute)
		defer deadline.Stop()
		var events []*db.AgentEvent
		for ctx.Err() == nil {
			run, err := q.GetAgentRun(ctx, &db.GetAgentRunParams{ID: id, UserID: owner})
			if err != nil {
				return nil
			}
			rows, err := q.ListAgentEvents(ctx, &db.ListAgentEventsParams{RunID: id, UserID: owner, ID: after})
			if err != nil {
				return err
			}
			events = append(events, rows...)
			if len(rows) > 0 {
				after = rows[len(rows)-1].ID
			}
			if len(events) > 1000 {
				events = events[len(events)-1000:]
			}
			var messages []modelruntime.Message
			_ = json.Unmarshal(run.Messages, &messages)
			if err = sse.PatchElementTempl(templates.AgentRunView(run, messages, events)); err != nil {
				return nil
			}
			if run.Status != "queued" && run.Status != "running" && run.Status != "waiting_capacity" && run.Status != "waiting_input" && run.Status != "waiting_approval" {
				return nil
			}
			select {
			case <-ctx.Done():
				return nil
			case <-deadline.C:
				return nil
			case <-time.After(time.Second):
			}
		}
		return nil
	}, guard)
	e.POST("/api/agent/runs/:id/stop", func(c echo.Context) error {
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		owner, _, _ := common.RequireSessionUser(c, sm)
		ctx := c.Request().Context()
		if err = dbc.Queries(ctx).CancelAgentRun(ctx, &db.CancelAgentRunParams{ID: id, UserID: owner}); err != nil {
			return err
		}
		return datastar.NewSSE(c.Response(), c.Request()).PatchSignals([]byte(`{"agentStatus":"Stopping inference; submitted jobs remain available."}`))
	}, guard)
	e.POST("/api/agent/runs/:id/continue", func(c echo.Context) error {
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		owner, _, _ := common.RequireSessionUser(c, sm)
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		previous, err := q.GetAgentRun(ctx, &db.GetAgentRunParams{ID: id, UserID: owner})
		if err != nil {
			return echo.NewHTTPError(404)
		}
		if previous.Status != "limited" && previous.Status != "interrupted" && previous.Status != "cancelled" {
			return echo.NewHTTPError(409, "run is not continuable")
		}
		calls, err := q.ListAgentToolCalls(ctx, id)
		if err != nil {
			return err
		}
		messages, err := agent.ContinuationMessages(previous, calls)
		if err != nil {
			return echo.NewHTTPError(409, "Uncertain operation requires artifact reconciliation before continuation")
		}
		run, err := q.ContinueAgentRun(ctx, &db.ContinueAgentRunParams{ID: previous.ID, UserID: owner, Messages: messages})
		if err != nil {
			return echo.NewHTTPError(409, "conversation already active")
		}
		return datastar.NewSSE(c.Response(), c.Request()).PatchElementTempl(templates.AgentRunWatch(run.ID.String()))
	}, guard)
}
