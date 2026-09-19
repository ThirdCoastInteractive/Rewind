// Package runtime_api exposes authenticated operational settings and model controls.
package runtime_api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/modelruntime"
	"thirdcoast.systems/rewind/internal/runtimecfg"
)

// Register installs session-authenticated settings and model endpoints.
func Register(e *echo.Echo, sm *auth.SessionManager, dbc *db.DatabaseConnection) {
	guard := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			id, _, err := common.RequireSessionUser(c, sm)
			if err != nil {
				return echo.NewHTTPError(401, "authentication required")
			}
			if err := runtimecfg.RequireAdmin(c.Request().Context(), dbc, id); err != nil {
				return echo.NewHTTPError(403, "administrator access required")
			}
			return next(c)
		}
	}
	render := func(c echo.Context, message string) error {
		ctx := c.Request().Context()
		values, err := runtimecfg.Read(ctx, dbc)
		if err != nil {
			return err
		}
		consumers, err := dbc.Queries(ctx).ListRuntimeConsumers(ctx)
		if err != nil {
			return err
		}
		consumers = runtimecfg.LiveConsumers(consumers, time.Now())
		return datastar.NewSSE(c.Response(), c.Request()).PatchElementTempl(templates.LiveSettings(values, consumers, message))
	}
	e.GET("/api/settings/live", func(c echo.Context) error {
		if c.QueryParam("render") == "1" {
			return render(c, "")
		}
		ctx := c.Request().Context()
		values, err := runtimecfg.Read(ctx, dbc)
		if err != nil {
			return err
		}
		saved, err := dbc.Queries(ctx).ListRuntimeSettings(ctx)
		if err != nil {
			return err
		}
		consumers, err := dbc.Queries(ctx).ListRuntimeConsumers(ctx)
		if err != nil {
			return err
		}
		consumers = runtimecfg.LiveConsumers(consumers, time.Now())
		return c.JSON(200, map[string]any{"definitions": runtimecfg.Registry, "effective": values, "saved": saved, "services": consumers, "deployment_managed": []string{"container images", "GPU passthrough", "mounts", "ports", "database credentials", "bootstrap secrets"}})
	}, guard)
	e.PUT("/api/settings/live", func(c echo.Context) error {
		var in runtimecfg.Snapshot
		if err := json.NewDecoder(http.MaxBytesReader(c.Response(), c.Request().Body, 64<<10)).Decode(&in); err != nil {
			return echo.NewHTTPError(400, "invalid settings object")
		}
		id, _, _ := common.RequireSessionUser(c, sm)
		if err := runtimecfg.Save(c.Request().Context(), dbc, id, in); err != nil {
			return echo.NewHTTPError(400, err.Error())
		}
		return c.JSON(200, map[string]string{"status": "saved"})
	}, guard)
	e.PUT("/api/settings/live/:key", func(c echo.Context) error {
		key := c.Param("key")
		raw := c.QueryParam("value")
		var value any = raw
		for _, d := range runtimecfg.Registry {
			if d.Key == key {
				switch d.Kind {
				case "boolean":
					v, err := strconv.ParseBool(raw)
					if err != nil {
						return render(c, "Invalid boolean")
					}
					value = v
				case "number":
					v, err := strconv.ParseFloat(raw, 64)
					if err != nil {
						return render(c, "Invalid number")
					}
					value = v
				}
			}
		}
		id, _, _ := common.RequireSessionUser(c, sm)
		if err := runtimecfg.Save(c.Request().Context(), dbc, id, runtimecfg.Snapshot{key: value}); err != nil {
			return render(c, err.Error())
		}
		return render(c, "Saved. Services apply changes at their next job or checkpoint.")
	}, guard)
	e.GET("/api/models", func(c echo.Context) error {
		result, err := modelruntime.Request(c.Request().Context(), "/v1/models", nil)
		if err != nil {
			return echo.NewHTTPError(503, err.Error())
		}
		return c.Blob(200, "application/json", result)
	}, guard)
	e.POST("/api/models/operations", func(c echo.Context) error {
		var in struct {
			Runtime       string `json:"runtime"`
			Model         string `json:"model"`
			Action        string `json:"action"`
			AcceptLicense bool   `json:"accept_license"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(c.Response(), c.Request().Body, 8192)).Decode(&in); err != nil {
			return echo.NewHTTPError(400, "invalid operation")
		}
		id, _, _ := common.RequireSessionUser(c, sm)
		op, err := modelruntime.Enqueue(c.Request().Context(), dbc, id, in.Runtime, in.Model, in.Action, map[string]any{"accept_license": in.AcceptLicense})
		if err != nil {
			return echo.NewHTTPError(400, err.Error())
		}
		return c.JSON(202, op)
	}, guard)
	e.POST("/api/models/action", func(c echo.Context) error {
		id, _, _ := common.RequireSessionUser(c, sm)
		_, err := modelruntime.Enqueue(c.Request().Context(), dbc, id, c.QueryParam("runtime"), c.QueryParam("model"), c.QueryParam("action"), map[string]any{"accept_license": c.QueryParam("accept_license") == "true"})
		message := "Model operation queued. Progress appears below."
		if err != nil {
			message = err.Error()
		}
		return datastar.NewSSE(c.Response(), c.Request()).PatchElementTempl(templates.ModelActionStatus(message))
	}, guard)
	e.POST("/api/models/assign", func(c echo.Context) error {
		ctx := c.Request().Context()
		id, _, _ := common.RequireSessionUser(c, sm)
		key, model := c.QueryParam("key"), c.QueryParam("model")
		if key != "agent.model" && key != "ml.context_model" && key != "whisper.model" {
			return echo.NewHTTPError(400, "Unsupported assignment")
		}
		inventory, err := modelruntime.Request(ctx, "/v1/models", nil)
		if err != nil {
			return echo.NewHTTPError(503, "Model runtime unavailable")
		}
		if err = validateAssignment(inventory, key, model); err != nil {
			return datastar.NewSSE(c.Response(), c.Request()).PatchElementTempl(templates.ModelActionStatus(err.Error()))
		}
		if err = runtimecfg.Save(ctx, dbc, id, runtimecfg.Snapshot{key: model}); err != nil {
			return echo.NewHTTPError(400, err.Error())
		}
		return render(c, "Assigned "+model+". Applies to subsequent work.")
	}, guard)
	e.POST("/settings/models", func(c echo.Context) error {
		id, _, _ := common.RequireSessionUser(c, sm)
		_, err := modelruntime.Enqueue(c.Request().Context(), dbc, id, c.FormValue("runtime"), c.FormValue("model"), c.FormValue("action"), map[string]any{"accept_license": c.FormValue("accept_license") == "true"})
		if err != nil {
			return echo.NewHTTPError(400, err.Error())
		}
		return c.Redirect(303, "/admin/settings#settings-models")
	}, guard)
	e.GET("/api/models/operations", func(c echo.Context) error {
		ctx := c.Request().Context()
		if c.QueryParam("render") != "1" {
			rows, err := dbc.Queries(ctx).ListModelOperations(ctx)
			if err != nil {
				return err
			}
			return c.JSON(200, rows)
		}
		sse := datastar.NewSSE(c.Response(), c.Request())
		for ctx.Err() == nil {
			id, _, err := common.RequireSessionUser(c, sm)
			if err != nil {
				return nil
			}
			if runtimecfg.RequireAdmin(ctx, dbc, id) != nil {
				return nil
			}
			rows, err := dbc.Queries(ctx).ListModelOperations(ctx)
			if err != nil {
				return err
			}
			inventory, err := modelruntime.Request(ctx, "/v1/models", nil)
			message := ""
			if err != nil {
				message = fmt.Sprint(err)
			}
			if values, readErr := runtimecfg.Read(ctx, dbc); readErr == nil {
				var data map[string]any
				if json.Unmarshal(inventory, &data) == nil && data != nil {
					data["assignments"] = values
					inventory, _ = json.Marshal(data)
				}
			}
			if err := sse.PatchElementTempl(templates.ModelInventory(inventory, rows, message)); err != nil {
				return nil
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(5 * time.Second):
			}
		}
		return nil
	}, guard)
}

func validateAssignment(raw []byte, key, model string) error {
	var in struct {
		Ollama  struct{ Models []struct{ Name string } }
		Details map[string]struct{ Capabilities []string }
		Whisper []struct {
			Name      string
			Installed bool
		}
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return fmt.Errorf("could not read model inventory")
	}
	if key == "whisper.model" {
		for _, m := range in.Whisper {
			if m.Name == model && m.Installed {
				return nil
			}
		}
	} else {
		required := "completion"
		if key == "agent.model" {
			required = "tools"
		}
		for _, m := range in.Ollama.Models {
			if m.Name == model {
				for _, cap := range in.Details[model].Capabilities {
					if cap == required {
						return nil
					}
				}
			}
		}
	}
	return fmt.Errorf("model must be installed and support this task before assignment")
}
