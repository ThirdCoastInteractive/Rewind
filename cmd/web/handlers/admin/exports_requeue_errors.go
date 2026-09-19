package admin

import (
	"log/slog"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleAdminExportsRequeueErrors serves POST /admin/exports/requeue-errors for the selected queue.
func HandleAdminExportsRequeueErrors(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		kind := exportKind(c)

		if kind == "stitch" {
			if err := q.RequeueAllErrorStitchExports(ctx); err != nil {
				slog.Error("failed to requeue stitch error exports", "error", err)
				return c.String(500, "failed to requeue exports")
			}
			_, _ = dbc.Exec(ctx, "SELECT pg_notify('stitch_jobs', 'requeue')")
			return c.Redirect(303, "/admin/exports?"+exportKindQuery(kind)+"&alert=success&msg=Stitch+error+exports+requeued")
		}

		if err := q.RequeueAllErrorExports(ctx); err != nil {
			slog.Error("failed to requeue clip error exports", "error", err)
			return c.String(500, "failed to requeue exports")
		}
		_, _ = dbc.Exec(ctx, "SELECT pg_notify('clip_exports', 'requeue')")
		return c.Redirect(303, "/admin/exports?"+exportKindQuery(kind)+"&alert=success&msg=Clip+error+exports+requeued")
	}
}
