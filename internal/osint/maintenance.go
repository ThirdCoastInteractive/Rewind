package osint

import (
	"context"
	"log/slog"
	"strconv"

	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/runtimecfg"
)

// RunMaintenance is idempotent, batch-limited OSINT detector work safe to call
// every minute from rewind-ml runMLMaintenance. Observe/flag only.
func RunMaintenance(ctx context.Context, dbc *db.DatabaseConnection) error {
	if dbc == nil || dbc.Pool == nil {
		return nil
	}
	s := store{pool: dbc.Pool}
	ready, err := s.tablesReady(ctx)
	if err != nil {
		return err
	}
	if !ready {
		slog.Debug("osint tables not present; skipping maintenance")
		return nil
	}

	cfg := settings{
		toxicityFlag: floatSetting(ctx, "osint.toxicity_flag", 0.7),
		raidRatio:    floatSetting(ctx, "osint.raid_ratio", 4),
		styleMinN:    runtimecfg.Int(ctx, "osint.style_min_n"),
	}
	if cfg.styleMinN < 3 {
		cfg.styleMinN = 8
	}

	steps := []struct {
		name string
		fn   func() error
	}{
		{"fill_simhash", func() error { return s.fillMissingSimhashes(ctx) }},
		{"refresh_styles", func() error { return s.refreshCommenterStyles(ctx) }},
		{"copypaste", func() error { return s.detectCopyPasteCampaigns(ctx) }},
		{"raid", func() error { return s.detectRaids(ctx, cfg.raidRatio) }},
		{"newcomer", func() error { return s.detectNewcomers(ctx, cfg.toxicityFlag) }},
		{"toxicity_burst", func() error { return s.detectToxicityBursts(ctx, cfg.toxicityFlag) }},
		{"sock_suggest", func() error { return s.detectSockSuggestions(ctx, cfg.styleMinN) }},
		{"commented_by_edges", func() error { return s.maintainCommentedByEdges(ctx) }},
	}
	for _, step := range steps {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := step.fn(); err != nil {
			// Keep going across independent detectors; one missing column should not abort all.
			slog.Warn("osint maintenance step failed", "step", step.name, "error", err)
		}
	}
	return nil
}

func floatSetting(ctx context.Context, key string, fallback float64) float64 {
	f, err := strconv.ParseFloat(runtimecfg.String(ctx, key), 64)
	if err != nil {
		return fallback
	}
	return f
}
