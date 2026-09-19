package compilation_api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/compilation"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/events"
)

// Register installs compilation list, preview, segment, and render routes.
func Register(e *echo.Echo, sm *auth.SessionManager, dbc *db.DatabaseConnection) {
	guard := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if _, _, err := common.RequireSessionUser(c, sm); err != nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
			}
			return next(c)
		}
	}
	e.GET("/compilations", func(c echo.Context) error {
		user, name, _ := common.RequireSessionUser(c, sm)
		plans, err := dbc.Queries(c.Request().Context()).ListCompilationPlansForUser(c.Request().Context(), &db.ListCompilationPlansForUserParams{CreatedBy: user, PageLimit: 100})
		if err != nil {
			return err
		}
		return templates.CompilationsPage(name, plans).Render(c.Request().Context(), c.Response())
	}, guard)
	e.GET("/compilations/:id", func(c echo.Context) error {
		user, name, _ := common.RequireSessionUser(c, sm)
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		plan, err := dbc.Queries(c.Request().Context()).GetCompilationPlan(c.Request().Context(), id)
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound, "plan not found")
		}
		if plan.CreatedBy != user {
			return echo.NewHTTPError(http.StatusForbidden, "plan access denied")
		}
		segments, err := dbc.Queries(c.Request().Context()).ListCompilationPlanSegments(c.Request().Context(), id)
		if err != nil {
			return err
		}
		return templates.CompilationDetailPage(name, plan, segments).Render(c.Request().Context(), c.Response())
	}, guard)
	e.GET("/api/compilations/:id/events", func(c echo.Context) error {
		user, _, _ := common.RequireSessionUser(c, sm)
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		ctx := c.Request().Context()
		wake, unsubscribe := events.Default.Subscribe()
		defer unsubscribe()
		sse := datastar.NewSSE(c.Response(), c.Request())
		keepalive := time.NewTicker(15 * time.Second)
		defer keepalive.Stop()
		timeout := time.NewTimer(2 * time.Hour)
		defer timeout.Stop()
		patch := func() error {
			plan, err := dbc.Queries(ctx).GetCompilationPlan(ctx, id)
			if err != nil {
				return err
			}
			if plan.CreatedBy != user {
				return echo.NewHTTPError(http.StatusForbidden, "plan access denied")
			}
			segments, err := dbc.Queries(ctx).ListCompilationPlanSegments(ctx, id)
			if err != nil {
				return err
			}
			runs, err := dbc.Queries(ctx).ListCompilationExecutions(ctx, id)
			if err != nil {
				return err
			}
			return sse.PatchElementTempl(templates.CompilationLive(plan, segments, runs))
		}
		if err := patch(); err != nil {
			return err
		}
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-timeout.C:
				return nil
			case <-keepalive.C:
				if err := common.WriteSSEKeepalive(c); err != nil {
					return nil
				}
			case <-wake:
				if err := patch(); err != nil {
					return err
				}
			}
		}
	}, guard)
	e.POST("/api/compilations/segments", func(c echo.Context) error {
		user, _, _ := common.RequireSessionUser(c, sm)
		ctx := c.Request().Context()
		videoID, err := common.ParseUUID(c.FormValue("video_id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "video_id required")
		}
		start, err := strconv.ParseFloat(c.FormValue("start"), 64)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid start")
		}
		end, err := strconv.ParseFloat(c.FormValue("end"), 64)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid end")
		}
		title := strings.TrimSpace(c.FormValue("title"))
		if title == "" {
			title = "Visual moment"
		}
		q, tx, err := dbc.NewWithTX(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		plan, err := q.CreateCompilationPlan(ctx, &db.CreateCompilationPlanParams{CreatedBy: user, Title: title, SourceQuery: "visual"})
		if err != nil {
			return err
		}
		evidence, _ := json.Marshal(map[string]any{"source": "visual_search"})
		if _, err = q.AddCompilationPlanSegment(ctx, &db.AddCompilationPlanSegmentParams{PlanID: plan.ID, Position: 0, VideoID: videoID, StartTs: start, EndTs: end, MatchEvidence: evidence, SelectionRationale: "visual search"}); err != nil {
			return err
		}
		if _, err = q.SetInitialCompilationPlanDuration(ctx, &db.SetInitialCompilationPlanDurationParams{ID: plan.ID, EstimatedDuration: end - start}); err != nil {
			return err
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
		return c.Redirect(http.StatusSeeOther, "/compilations/"+plan.ID.String())
	}, guard)
	e.POST("/api/compilations/:id/render", func(c echo.Context) error {
		user, _, _ := common.RequireSessionUser(c, sm)
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		revision, err := strconv.Atoi(c.FormValue("revision"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "revision required")
		}
		if _, err = compilation.Execute(c.Request().Context(), dbc, id, user, int32(revision), c.FormValue("retry") == "true"); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		return c.Redirect(http.StatusSeeOther, "/compilations/"+id.String())
	}, guard)
}
