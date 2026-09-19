// Package analyze computes public-data decline signals for a creator or channel.
// Ported from creator-autopsy; it has no I/O. Metrics are only as fresh as
// Rewind's stored view/like/comment counts.
package analyze

import (
	"fmt"
	"sort"
	"time"
)

// Video is one archived item used as Analyze input. Counts are only as fresh
// as Rewind's stored metadata.
type Video struct {
	ID              string
	Platform        string
	Format          string // "video" | "livestream" | "short"
	Title           string
	UploadDate      time.Time
	DurationSeconds int
	ViewCount       int64
	LikeCount       int64
	CommentCount    int64
}

// Signal is a human-readable finding at a severity level.
type Signal struct {
	Level  string // "info" | "warning" | "bad"
	Format string // "video" | "livestream" | "short" | "" (overall)
	Text   string
}

// FormatStats compares the historical "winners" window against recent uploads
// for one format (or "overall").
type FormatStats struct {
	Format string

	OldCount    int
	RecentCount int

	OldAvgViewsPerDay       float64
	RecentAvgViewsPerDay    float64
	OldMedianViewsPerDay    float64
	RecentMedianViewsPerDay float64

	OldAvgDurationSeconds    float64
	RecentAvgDurationSeconds float64

	OldLikesPer1k    float64
	RecentLikesPer1k float64

	OldCommentsPer1k    float64
	RecentCommentsPer1k float64

	OldUploadsPerWeek    float64
	RecentUploadsPerWeek float64
}

// Report is the Analyze result: a status rollup, signals, and per-format stats.
type Report struct {
	Status  string // "fine" | "drifting" | "dying"
	Signals []Signal
	Stats   map[string]FormatStats

	OldFormatMix    map[string]float64
	RecentFormatMix map[string]float64
}

const (
	recentDays     = 90
	oldExcludeDays = 30
	oldTopN        = 50
	minSample      = 3
)

// Analyze compares recent uploads against a historical baseline and returns
// decline signals. It has no I/O; callers supply already-loaded videos.
func Analyze(videos []Video, now time.Time) Report {
	r := Report{
		Stats:           make(map[string]FormatStats),
		OldFormatMix:    make(map[string]float64),
		RecentFormatMix: make(map[string]float64),
	}

	if len(videos) == 0 {
		r.Status = "fine"
		r.Signals = []Signal{{Level: "info", Format: "", Text: "no videos to analyze"}}
		return r
	}

	formats := []string{"video", "livestream", "short"}
	byFormat := make(map[string][]Video)
	for _, v := range videos {
		byFormat[v.Format] = append(byFormat[v.Format], v)
	}

	for _, f := range formats {
		fVideos := byFormat[f]
		winners := topOld(fVideos, now)
		rec := recent(fVideos, now)
		allOldVids := allOld(fVideos, now)

		stats := computeStats(winners, rec, allOldVids, f, now)
		r.Stats[f] = stats
		r.Signals = append(r.Signals, generateSignals(stats)...)
	}

	winners := topOld(videos, now)
	rec := recent(videos, now)
	allOldVids := allOld(videos, now)
	overall := computeStats(winners, rec, allOldVids, "overall", now)
	r.Stats["overall"] = overall
	r.Signals = append(r.Signals, generateSignals(overall)...)

	r.OldFormatMix = formatMix(allOld(videos, now))
	r.RecentFormatMix = formatMix(recent(videos, now))
	r.Signals = append(r.Signals, mixShiftSignals(r.OldFormatMix, r.RecentFormatMix)...)

	r.Status = "fine"
	for _, s := range r.Signals {
		if s.Level == "bad" {
			r.Status = "dying"
			break
		}
		if s.Level == "warning" {
			r.Status = "drifting"
		}
	}

	return r
}

func viewsPerDay(v Video, now time.Time) float64 {
	days := now.Sub(v.UploadDate).Hours() / 24
	if days < 1 {
		days = 1
	}
	return float64(v.ViewCount) / days
}

func topOld(videos []Video, now time.Time) []Video {
	cutoff := now.AddDate(0, 0, -oldExcludeDays)
	var eligible []Video
	for _, v := range videos {
		if v.UploadDate.Before(cutoff) {
			eligible = append(eligible, v)
		}
	}
	sort.Slice(eligible, func(i, j int) bool {
		return viewsPerDay(eligible[i], now) > viewsPerDay(eligible[j], now)
	})
	if len(eligible) > oldTopN {
		eligible = eligible[:oldTopN]
	}
	return eligible
}

func allOld(videos []Video, now time.Time) []Video {
	cutoff := now.AddDate(0, 0, -oldExcludeDays)
	var out []Video
	for _, v := range videos {
		if v.UploadDate.Before(cutoff) {
			out = append(out, v)
		}
	}
	return out
}

func recent(videos []Video, now time.Time) []Video {
	cutoff := now.AddDate(0, 0, -recentDays)
	var out []Video
	for _, v := range videos {
		if !v.UploadDate.Before(cutoff) {
			out = append(out, v)
		}
	}
	return out
}

func computeStats(winners, rec, oldAll []Video, format string, now time.Time) FormatStats {
	fs := FormatStats{
		Format:      format,
		OldCount:    len(winners),
		RecentCount: len(rec),
	}

	oldVPDs := vpds(winners, now)
	recVPDs := vpds(rec, now)

	fs.OldAvgViewsPerDay = avg(oldVPDs)
	fs.RecentAvgViewsPerDay = avg(recVPDs)
	fs.OldMedianViewsPerDay = median(oldVPDs)
	fs.RecentMedianViewsPerDay = median(recVPDs)

	fs.OldAvgDurationSeconds = avgDuration(winners)
	fs.RecentAvgDurationSeconds = avgDuration(rec)

	fs.OldLikesPer1k = likesPer1k(winners)
	fs.RecentLikesPer1k = likesPer1k(rec)

	fs.OldCommentsPer1k = commentsPer1k(winners)
	fs.RecentCommentsPer1k = commentsPer1k(rec)

	fs.OldUploadsPerWeek = uploadsPerWeek(oldAll, now)
	fs.RecentUploadsPerWeek = uploadsPerWeekRecent(rec)

	return fs
}

func generateSignals(stats FormatStats) []Signal {
	format := stats.Format
	if stats.OldCount < minSample || stats.RecentCount < minSample {
		return []Signal{{
			Level:  "info",
			Format: format,
			Text:   fmt.Sprintf("insufficient %s data (old: %d, recent: %d)", format, stats.OldCount, stats.RecentCount),
		}}
	}

	var signals []Signal

	if stats.OldAvgViewsPerDay > 0 {
		ratio := stats.RecentAvgViewsPerDay / stats.OldAvgViewsPerDay
		if ratio < 0.5 {
			signals = append(signals, Signal{
				Level:  "bad",
				Format: format,
				Text:   fmt.Sprintf("%s views/day collapsed %.0f%% (%.0f → %.0f)", format, (1-ratio)*100, stats.OldAvgViewsPerDay, stats.RecentAvgViewsPerDay),
			})
		} else if ratio < 0.75 {
			signals = append(signals, Signal{
				Level:  "warning",
				Format: format,
				Text:   fmt.Sprintf("%s views/day declining %.0f%% (%.0f → %.0f)", format, (1-ratio)*100, stats.OldAvgViewsPerDay, stats.RecentAvgViewsPerDay),
			})
		}
	}

	if stats.OldAvgDurationSeconds > 0 && stats.RecentAvgDurationSeconds > stats.OldAvgDurationSeconds*1.5 {
		signals = append(signals, Signal{
			Level:  "warning",
			Format: format,
			Text:   fmt.Sprintf("%s duration drifted up %.0f%% (%.0fs → %.0fs)", format, (stats.RecentAvgDurationSeconds/stats.OldAvgDurationSeconds-1)*100, stats.OldAvgDurationSeconds, stats.RecentAvgDurationSeconds),
		})
	}

	if stats.OldUploadsPerWeek > 0 {
		if stats.RecentUploadsPerWeek == 0 {
			signals = append(signals, Signal{
				Level:  "bad",
				Format: format,
				Text:   fmt.Sprintf("%s uploads collapsed to zero (was %.1f/week)", format, stats.OldUploadsPerWeek),
			})
		} else if stats.RecentUploadsPerWeek < stats.OldUploadsPerWeek*0.5 {
			signals = append(signals, Signal{
				Level:  "warning",
				Format: format,
				Text:   fmt.Sprintf("%s cadence dropped %.0f%% (%.1f → %.1f/week)", format, (1-stats.RecentUploadsPerWeek/stats.OldUploadsPerWeek)*100, stats.OldUploadsPerWeek, stats.RecentUploadsPerWeek),
			})
		}
	}

	if stats.OldLikesPer1k > 0 && stats.RecentLikesPer1k < stats.OldLikesPer1k*0.65 {
		signals = append(signals, Signal{
			Level:  "warning",
			Format: format,
			Text:   fmt.Sprintf("%s likes/1k dropped (%.1f → %.1f)", format, stats.OldLikesPer1k, stats.RecentLikesPer1k),
		})
	}

	if stats.OldCommentsPer1k > 0 && stats.RecentCommentsPer1k < stats.OldCommentsPer1k*0.65 {
		signals = append(signals, Signal{
			Level:  "warning",
			Format: format,
			Text:   fmt.Sprintf("%s comments/1k dropped (%.1f → %.1f)", format, stats.OldCommentsPer1k, stats.RecentCommentsPer1k),
		})
	}

	return signals
}

func mixShiftSignals(oldMix, recentMix map[string]float64) []Signal {
	var signals []Signal

	if recentMix["livestream"]-oldMix["livestream"] > 0.25 {
		signals = append(signals, Signal{
			Level:  "warning",
			Format: "",
			Text:   fmt.Sprintf("format mix shifting to livestreams (%.0f%% → %.0f%%)", oldMix["livestream"]*100, recentMix["livestream"]*100),
		})
	}
	if recentMix["short"]-oldMix["short"] > 0.25 {
		signals = append(signals, Signal{
			Level:  "warning",
			Format: "",
			Text:   fmt.Sprintf("format mix shifting to shorts (%.0f%% → %.0f%%)", oldMix["short"]*100, recentMix["short"]*100),
		})
	}
	if oldMix["video"]-recentMix["video"] > 0.30 {
		signals = append(signals, Signal{
			Level:  "bad",
			Format: "",
			Text:   fmt.Sprintf("long-form abandoned (%.0f%% → %.0f%%)", oldMix["video"]*100, recentMix["video"]*100),
		})
	}

	return signals
}

func formatMix(videos []Video) map[string]float64 {
	mix := map[string]float64{"video": 0, "livestream": 0, "short": 0}
	if len(videos) == 0 {
		return mix
	}
	counts := map[string]int{"video": 0, "livestream": 0, "short": 0}
	for _, v := range videos {
		counts[v.Format]++
	}
	total := float64(len(videos))
	for k, c := range counts {
		mix[k] = float64(c) / total
	}
	return mix
}

func vpds(videos []Video, now time.Time) []float64 {
	out := make([]float64, len(videos))
	for i, v := range videos {
		out[i] = viewsPerDay(v, now)
	}
	return out
}

func avg(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	var sum float64
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

func median(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 0 {
		return (sorted[n/2-1] + sorted[n/2]) / 2
	}
	return sorted[n/2]
}

func avgDuration(videos []Video) float64 {
	if len(videos) == 0 {
		return 0
	}
	var sum float64
	for _, v := range videos {
		sum += float64(v.DurationSeconds)
	}
	return sum / float64(len(videos))
}

func likesPer1k(videos []Video) float64 {
	var totalViews, totalLikes int64
	for _, v := range videos {
		totalViews += v.ViewCount
		totalLikes += v.LikeCount
	}
	if totalViews == 0 {
		return 0
	}
	return float64(totalLikes) / float64(totalViews) * 1000
}

func commentsPer1k(videos []Video) float64 {
	var totalViews, totalComments int64
	for _, v := range videos {
		totalViews += v.ViewCount
		totalComments += v.CommentCount
	}
	if totalViews == 0 {
		return 0
	}
	return float64(totalComments) / float64(totalViews) * 1000
}

func uploadsPerWeek(videos []Video, now time.Time) float64 {
	if len(videos) == 0 {
		return 0
	}
	var earliest time.Time
	for _, v := range videos {
		if earliest.IsZero() || v.UploadDate.Before(earliest) {
			earliest = v.UploadDate
		}
	}
	weeks := now.Sub(earliest).Hours() / 24 / 7
	if weeks < 1 {
		weeks = 1
	}
	return float64(len(videos)) / weeks
}

func uploadsPerWeekRecent(videos []Video) float64 {
	return float64(len(videos)) / (float64(recentDays) / 7)
}
