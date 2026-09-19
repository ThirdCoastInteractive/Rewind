// Package vision_api serves visual retrieval and frames.
package vision_api

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/frames"
	"thirdcoast.systems/rewind/internal/jsnum"
	"thirdcoast.systems/rewind/internal/vision"
)

// Register installs authenticated visual tools and pages on the application router.
func Register(e *echo.Echo, sm *auth.SessionManager, dbc *db.DatabaseConnection) {
	guard := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if _, _, err := common.RequireSessionUser(c, sm); err != nil {
				return echo.NewHTTPError(401, "authentication required")
			}
			return next(c)
		}
	}
	e.GET("/visual", func(c echo.Context) error {
		_, name, _ := common.RequireSessionUser(c, sm)
		ctx := c.Request().Context()
		channels, err := dbc.Queries(ctx).ListVisionChannels(ctx)
		if err != nil {
			return err
		}
		creators, err := dbc.Queries(ctx).ListVisionCreators(ctx)
		if err != nil {
			return err
		}
		return templates.VisualPage(name, channels, creators).Render(ctx, c.Response())
	}, guard)
	e.GET("/api/visual/search", func(c echo.Context) error {
		owner, _, _ := common.RequireSessionUser(c, sm)
		in := vision.SearchInput{Text: c.QueryParam("q"), ReferenceID: c.QueryParam("reference_id"), FrameRef: c.QueryParam("frame_ref"), VideoID: c.QueryParam("video_id"), ChannelID: c.QueryParam("channel_id"), CreatorID: c.QueryParam("creator_id")}
		rows, err := vision.Search(c.Request().Context(), dbc, owner, in)
		if c.QueryParam("render") == "1" {
			msg := ""
			if err != nil {
				msg = err.Error()
			}
			return datastar.NewSSE(c.Response(), c.Request()).PatchElementTempl(templates.VisualResults(rows, msg))
		}
		if err != nil {
			return echo.NewHTTPError(400, err.Error())
		}
		return c.JSON(200, rows)
	}, guard)
	e.POST("/api/visual-index", func(c echo.Context) error {
		var in vision.IndexRange
		if strings.Contains(c.Request().Header.Get("Content-Type"), "application/json") {
			if err := c.Bind(&in); err != nil {
				return err
			}
		} else {
			in.VideoID = c.FormValue("video_id")
			in.Dense = c.FormValue("dense") == "true"
			if v := c.FormValue("start"); v != "" {
				n, err := strconv.ParseFloat(v, 64)
				if err != nil {
					return echo.NewHTTPError(400, "invalid start")
				}
				in.Start = jsnum.F(n)
			}
			if v := c.FormValue("end"); v != "" {
				n, err := strconv.ParseFloat(v, 64)
				if err != nil {
					return echo.NewHTTPError(400, "invalid end")
				}
				in.End = jsnum.F(n)
			}
		}
		id, err := vision.QueueRange(c.Request().Context(), dbc, in)
		if err != nil {
			return echo.NewHTTPError(400, err.Error())
		}
		if strings.Contains(c.Request().Header.Get("Content-Type"), "application/json") {
			return c.JSON(202, map[string]any{"job_id": id.String()})
		}
		return c.Redirect(303, "/visual")
	}, guard)
	e.POST("/api/visual-references", func(c echo.Context) error {
		owner, _, _ := common.RequireSessionUser(c, sm)
		c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, 8<<20)
		f, err := c.FormFile("image")
		if err != nil {
			return echo.NewHTTPError(400, "image is required")
		}
		file, err := f.Open()
		if err != nil {
			return err
		}
		defer file.Close()
		raw, err := io.ReadAll(io.LimitReader(file, 8<<20))
		if err != nil {
			return err
		}
		client := vision.FromEnv()
		model, err := vision.ModelReady(c.Request().Context(), dbc.Queries(c.Request().Context()), client, vision.CLIPModel)
		if err != nil {
			return err
		}
		prediction, err := client.Predict(c.Request().Context(), "", raw)
		if err != nil {
			return err
		}
		vector, err := vision.Normalize(prediction.CLIP)
		if err != nil {
			return err
		}
		ref, err := dbc.Queries(c.Request().Context()).CreateVisualReference(c.Request().Context(), &db.CreateVisualReferenceParams{OwnerID: owner, ModelID: model.Fingerprint, Embedding: vector})
		if err != nil {
			return err
		}
		if c.FormValue("redirect") == "1" {
			return c.Redirect(303, "/visual?reference_id="+ref.ID.String())
		}
		return c.JSON(201, ref)
	}, guard)
	e.GET("/api/videos/:id/frame", func(c echo.Context) error {
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		t, err := strconv.ParseFloat(c.QueryParam("t"), 64)
		if err != nil {
			return echo.NewHTTPError(400, "invalid timestamp")
		}
		asset, err := vision.Asset(c.Request().Context(), dbc.Queries(c.Request().Context()), id)
		if err != nil {
			return err
		}
		fs, err := frames.Default.Get(c.Request().Context(), asset, []float64{t}, c.QueryParam("quality"), 960)
		if err != nil {
			return echo.NewHTTPError(400, err.Error())
		}
		if fs[0].Error != "" {
			return echo.NewHTTPError(404, fs[0].Error)
		}
		c.Response().Header().Set("Cache-Control", "private, max-age=60")
		return c.Blob(200, "image/jpeg", fs[0].JPEG)
	}, guard)
}
