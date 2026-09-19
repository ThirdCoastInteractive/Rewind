package mcp

import (
	"context"
	"fmt"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/modelruntime"
	"thirdcoast.systems/rewind/internal/runtimecfg"
)

func registerSettingsTools(srv *mcpsdk.Server, dbc *db.DatabaseConnection) {
	admin := func(ctx context.Context, write bool) error {
		token := tokenFrom(ctx)
		if token == nil {
			return fmt.Errorf("authentication required")
		}
		if write {
			if err := requireWrite(ctx); err != nil {
				return err
			}
		}
		return runtimecfg.RequireAdmin(ctx, dbc, token.UserID)
	}
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "inspect_settings", Description: "Administrator only: inspect live settings, definitions, and service application status."}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ *struct{}) (*mcpsdk.CallToolResult, any, error) {
		if err := admin(ctx, false); err != nil {
			return nil, nil, err
		}
		values, err := runtimecfg.Read(ctx, dbc)
		if err != nil {
			return nil, nil, err
		}
		consumers, err := dbc.Queries(ctx).ListRuntimeConsumers(ctx)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"definitions": runtimecfg.Registry, "effective": values, "services": runtimecfg.LiveConsumers(consumers, time.Now())})
	})
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "update_settings", Description: "Administrator only: atomically update validated operational settings. Subsequent jobs use new values; no redeploy."}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in *struct {
		Values map[string]any `json:"values"`
	}) (*mcpsdk.CallToolResult, any, error) {
		if err := admin(ctx, true); err != nil {
			return nil, nil, err
		}
		if err := runtimecfg.Save(ctx, dbc, tokenFrom(ctx).UserID, in.Values); err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]string{"status": "saved"})
	})
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "inspect_models", Description: "Administrator only: list installed and loaded models, capabilities, active use, and model operations."}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ *struct{}) (*mcpsdk.CallToolResult, any, error) {
		if err := admin(ctx, false); err != nil {
			return nil, nil, err
		}
		inventory, err := modelruntime.Request(ctx, "/v1/models", nil)
		if err != nil {
			return nil, nil, err
		}
		operations, err := dbc.Queries(ctx).ListModelOperations(ctx)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"inventory": inventory, "operations": operations})
	})
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "manage_model", Description: "Administrator only: install, load, unload, test, or remove a model. Runtimes: ollama, whisper, vision. Assigned or active weights cannot be removed."}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in *struct {
		Runtime       string `json:"runtime"`
		Model         string `json:"model"`
		Action        string `json:"action"`
		AcceptLicense bool   `json:"accept_license,omitempty"`
	}) (*mcpsdk.CallToolResult, any, error) {
		if err := admin(ctx, true); err != nil {
			return nil, nil, err
		}
		op, err := modelruntime.Enqueue(ctx, dbc, tokenFrom(ctx).UserID, in.Runtime, in.Model, in.Action, map[string]any{"accept_license": in.AcceptLicense})
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(op)
	})
}
