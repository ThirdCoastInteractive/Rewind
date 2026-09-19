package download

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/runtimecfg"
	"thirdcoast.systems/rewind/pkg/encryption"
	"thirdcoast.systems/rewind/pkg/utils/crypto"
	"thirdcoast.systems/rewind/pkg/ytdlp"
)

const (
	defaultSpoolDir  = "/spool"
	defaultYTDLPPath = "/usr/local/bin/yt-dlp"
	maxWorkers       = 32
	idlePoll         = 30 * time.Second
)

// Start runs download workers and background loops until ctx is cancelled.
func Start(ctx context.Context, dbc *db.DatabaseConnection, conf *config.Config) error {
	if conf == nil {
		return errors.New("nil config")
	}

	spoolDir := strings.TrimSpace(os.Getenv("SPOOL_DIR"))
	if spoolDir == "" {
		spoolDir = defaultSpoolDir
	}
	if err := os.MkdirAll(filepath.Join(spoolDir, "downloads"), 0o755); err != nil {
		return fmt.Errorf("create spool dir: %w", err)
	}

	ytdlpPath := strings.TrimSpace(os.Getenv("YTDLP_PATH"))
	if ytdlpPath == "" {
		ytdlpPath = defaultYTDLPPath
	}

	if pinned := os.Getenv("YTDLP_PINNED"); pinned != "" {
		slog.Info("yt-dlp pinned at build time, skipping self-update")
	} else {
		ytdlpUpdateCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		if err := ytdlp.New().Update(ytdlpUpdateCtx); err != nil {
			slog.Warn("failed to update yt-dlp", "error", err)
		} else {
			slog.Info("yt-dlp updated successfully")
		}
		cancel()
	}

	encMgr, err := application.InitEncryptionManager()
	if err != nil {
		return fmt.Errorf("initialize encryption manager: %w", err)
	}

	if err := runtimecfg.Start(ctx, dbc, "downloader"); err != nil {
		return fmt.Errorf("live settings initialization: %w", err)
	}

	slog.Info("Recovering stuck download jobs from previous service instances")
	if err := dbc.Queries(ctx).RecoverStuckDownloadJobs(ctx); err != nil {
		slog.Error("failed to recover stuck download jobs", "error", err)
	}

	client := ytdlp.New()
	client.Path = ytdlpPath

	wake := make(chan struct{}, 1)
	signalWake := func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	go db.RunListenLoop(ctx, dbc, []string{"download_jobs"}, func(*pgconn.Notification) {
		signalWake()
	}, signalWake)

	workers := runtimecfg.Int(ctx, "downloads.workers")
	if workers <= 0 {
		workers = 2
	}
	slog.Info("Downloader workers started", "workers", workers, "max", maxWorkers)
	for i := 0; i < maxWorkers; i++ {
		go downloadWorker(ctx, dbc, client, spoolDir, encMgr, wake, i)
	}

	go commentCatchupLoop(ctx, dbc, encMgr, ytdlpPath)
	go metadataRefreshLoop(ctx, dbc, encMgr, ytdlpPath)
	go watchSchedulerLoop(ctx, dbc)
	go catalogCrawlLoop(ctx, dbc, client.Path)
	go subtitleBackfillLoop(ctx, dbc)

	<-ctx.Done()
	slog.Info("Downloader service stopping")
	return nil
}

func downloadWorker(ctx context.Context, dbc *db.DatabaseConnection, client *ytdlp.Client, spoolDir string, encMgr *encryption.Manager, wake <-chan struct{}, index int) {
	q := dbc.Queries(ctx)
	for {
		if ctx.Err() != nil {
			return
		}

		for {
			if !runtimecfg.WaitWorker(ctx, "downloads.workers", index) {
				return
			}
			job, err := q.DequeueDownloadJob(ctx)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					break
				}
				slog.Error("failed to dequeue download job", "error", err)
				time.Sleep(2 * time.Second)
				break
			}

			jobClient := ytdlp.New()
			jobClient.Path = client.Path
			jobClient.ExtraArgs = client.ExtraArgs

			if err := processDownloadJob(ctx, dbc, q, jobClient, spoolDir, encMgr, job); err != nil {
				jobID := uuidString(job.ID)

				var execErr *ytdlp.ExecError
				if errors.As(err, &execErr) {
					slog.Error("download job failed",
						"job_id", jobID,
						"error", err,
						"exit_code", execErr.ExitCode,
						"stdout", execErr.Stdout,
						"stderr", execErr.Stderr)
				} else {
					slog.Error("download job failed", "job_id", jobID, "error", err)
				}

				errMsg := err.Error()
				_ = q.MarkDownloadJobFailed(ctx, &db.MarkDownloadJobFailedParams{ID: job.ID, LastError: &errMsg})
				continue
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-time.After(idlePoll):
		}
	}
}

func processDownloadJob(ctx context.Context, dbc *db.DatabaseConnection, q *db.Queries, client *ytdlp.Client, spoolDir string, encMgr *encryption.Manager, job *db.DownloadJob) error {
	var captureErr error
	ctx, captureErr = runtimecfg.Job(ctx, dbc, "download", job.ID)
	if captureErr != nil {
		return captureErr
	}
	jobID := uuidString(job.ID)
	if jobID == "" {
		return errors.New("invalid job id")
	}

	client.EnableCookieJar = true
	defer func() {
		if strings.TrimSpace(client.UpdatedCookies) == "" {
			return
		}
		persistCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		persistNetscapeCookies(persistCtx, q, encMgr, job.ArchivedBy, client.UpdatedCookies)
	}()

	var lastPIDHeartbeat time.Time
	client.LogCallback = func(stream string, line string) {
		if err := q.InsertYtdlpLog(ctx, &db.InsertYtdlpLogParams{
			JobID:   job.ID,
			Stream:  db.LogStream(stream),
			Message: line,
		}); err != nil {
			slog.Warn("failed to insert ytdlp log", "job_id", jobID, "error", err)
		}
		// Throttle PID/updated_at heartbeats so long live downloads stay visible.
		if client.LastPID > 0 && time.Since(lastPIDHeartbeat) >= 30*time.Second {
			lastPIDHeartbeat = time.Now()
			lastPID := int64(client.LastPID)
			_ = q.UpdateDownloadJobPID(ctx, &db.UpdateDownloadJobPIDParams{ID: job.ID, ProcessPid: &lastPID})
		}
	}
	defer func() {
		client.LogCallback = nil
	}()

	cookies, err := q.GetUserCookies(ctx, job.ArchivedBy)
	if err != nil {
		slog.Warn("failed to get user cookies", "job_id", jobID, "user_id", job.ArchivedBy, "error", err)
		client.Cookies = cookiesHeader()
	} else if len(cookies) == 0 {
		slog.Warn("no cookies found for user", "job_id", jobID, "user_id", job.ArchivedBy)
		client.Cookies = cookiesHeader()
	} else {
		cookiesContent := generateCookiesFile(encMgr, cookies)
		if strings.TrimSpace(cookiesContent) == "" {
			cookiesContent = cookiesHeader()
		}
		client.Cookies = cookiesContent

		lines := strings.Split(cookiesContent, "\n")
		preview := ""
		if len(lines) > 5 {
			preview = strings.Join(lines[:3], "\n") + "\n...\n" + strings.Join(lines[len(lines)-3:], "\n")
		} else {
			preview = cookiesContent
		}

		slog.Info("Using cookies for authenticated download", "job_id", jobID, "user_id", job.ArchivedBy, "cookie_count", len(cookies), "cookies_bytes", len(cookiesContent), "line_count", len(lines))
		slog.Info("Cookies preview", "job_id", jobID, "preview", preview)
	}

	if job.Kind == "playlist" {
		return processPlaylistJob(ctx, dbc, q, client, job)
	}
	if job.Kind == "metadata-catalog" {
		return processMetadataCatalogJob(ctx, q, client, job)
	}

	destDir := filepath.Join(spoolDir, "downloads", jobID)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}

	var infoPath string
	if job.Kind == "metadata" {
		return processMetadataJob(ctx, q, client, job, destDir)
	}
	if job.Refresh {
		infoPath = filepath.Join(destDir, "refresh.info.json")
		slog.Info("Refreshing metadata", "job_id", jobID, "url", job.URL)
		if err := client.DumpInfoJSON(ctx, job.URL, infoPath, "--no-playlist"); err != nil {
			return err
		}

		if err := client.WriteThumbnail(ctx, job.URL, destDir); err != nil {
			var execErr *ytdlp.ExecError
			if errors.As(err, &execErr) {
				slog.Warn("failed to fetch thumbnail", "job_id", jobID, "error", err, "stderr", execErr.Stderr)
			} else {
				slog.Warn("failed to fetch thumbnail", "job_id", jobID, "error", err)
			}
		}
		if err := client.WriteSubtitles(ctx, job.URL, destDir); err != nil {
			recordSubtitleFetchError(destDir, err)
			var execErr *ytdlp.ExecError
			if errors.As(err, &execErr) {
				slog.Warn("failed to fetch subtitles", "job_id", jobID, "error", err, "stderr", execErr.Stderr)
			} else {
				slog.Warn("failed to fetch subtitles", "job_id", jobID, "error", err)
			}
		}

		if client.LastPID > 0 {
			lastPID := int64(client.LastPID)
			_ = q.UpdateDownloadJobPID(ctx, &db.UpdateDownloadJobPIDParams{ID: job.ID, ProcessPid: &lastPID})
		}
	} else {
		slog.Info("Downloading", "job_id", jobID, "url", job.URL)

		if maxH, err := q.GetMaxDownloadHeight(ctx); err != nil {
			if !db.IsUndefinedColumnErr(err) {
				slog.Warn("failed to read download quality cap; downloading uncapped", "job_id", jobID, "error", err)
			}
		} else if maxH > 0 {
			client.MaxHeight = int(maxH)
			slog.Info("Applying download quality cap", "job_id", jobID, "max_height", maxH)
		}

		mediaURL, extraArgs := ytdlp.StripRewindExtraArgs(job.ExtraArgs)
		liveOpts, hasLiveHint, extraArgs := ytdlp.ParseLiveExtraArgs(extraArgs)

		var probeLive bool
		if info, err := client.GetInfo(ctx, job.URL, "--no-playlist"); err != nil {
			slog.Warn("live probe failed; continuing as VOD", "job_id", jobID, "url", job.URL, "error", err)
		} else {
			probeLive = info.CurrentlyLive()
		}

		// --rewind-media-url is enqueue's live media sentinel; treat it as live-shaped.
		liveJob := probeLive || hasLiveHint || mediaURL != ""

		downloadURL := job.URL
		if mediaURL != "" {
			downloadURL = mediaURL
		}

		downloadArgs := []string{"--no-playlist"}
		if liveJob {
			client.Live = true
			downloadArgs = append(downloadArgs, ytdlp.LiveDownloadArgs(liveOpts)...)
			slog.Info("Live download mode", "job_id", jobID, "from_start", liveOpts.FromStart, "wait", liveOpts.Wait, "media_url_set", mediaURL != "")
		}
		if len(extraArgs) > 0 {
			downloadArgs = append(downloadArgs, extraArgs...)
		}
		if err := client.Download(ctx, downloadURL, destDir, downloadArgs...); err != nil {
			return err
		}

		if mediaURL != "" {
			pageInfoPath := filepath.Join(destDir, "page.info.json")
			if err := client.DumpInfoJSON(ctx, job.URL, pageInfoPath, "--no-playlist"); err != nil {
				slog.Warn("failed to dump page info.json for media-url download", "job_id", jobID, "error", err)
			} else {
				infoPath = pageInfoPath
			}
		}

		if infoPath == "" {
			infoMatches, err := filepath.Glob(filepath.Join(destDir, "*.info.json"))
			if err != nil {
				return err
			}
			if len(infoMatches) == 0 {
				return errors.New("yt-dlp did not produce .info.json")
			}
			// Prefer page.info.json when both media and page dumps exist.
			infoPath = infoMatches[0]
			for _, p := range infoMatches {
				if filepath.Base(p) == "page.info.json" {
					infoPath = p
					break
				}
			}
		}

		if client.LastPID > 0 {
			lastPID := int64(client.LastPID)
			_ = q.UpdateDownloadJobPID(ctx, &db.UpdateDownloadJobPIDParams{ID: job.ID, ProcessPid: &lastPID})
		}
	}

	if err := q.MarkDownloadJobSucceeded(ctx, &db.MarkDownloadJobSucceededParams{ID: job.ID, SpoolDir: &destDir, InfoJsonPath: &infoPath}); err != nil {
		return err
	}

	_, err = q.EnqueueIngestJob(ctx, job.ID)
	return err
}

func generateCookiesFile(encMgr *encryption.Manager, cookies []*db.GetUserCookiesRow) string {
	if len(cookies) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines, "# Netscape HTTP Cookie File")
	lines = append(lines, "# This is a generated file! Do not edit.")

	skipped := 0
	youtubeCookies := 0

	for _, cookie := range cookies {
		if strings.Contains(cookie.Domain, "youtube") {
			youtubeCookies++
		}

		var cookieValue crypto.EncryptedString = cookie.Value
		if err := encryption.Decrypt(encMgr, &cookieValue); err != nil {
			slog.Error("failed to decrypt cookie value", "error", err, "domain", cookie.Domain, "name", cookie.Name)
			skipped++
			continue
		}
		value, valid := cookieValue.Get()
		if !valid {
			slog.Warn("cookie value invalid after decryption", "domain", cookie.Domain, "name", cookie.Name)
			skipped++
			continue
		}

		line := fmt.Sprintf("%s\t%s\t%s\t%s\t%d\t%s\t%s",
			cookie.Domain,
			cookie.Flag,
			cookie.Path,
			cookie.Secure,
			cookie.Expiration,
			cookie.Name,
			value,
		)
		lines = append(lines, line)
	}

	slog.Info("Cookie generation complete", "total", len(cookies), "youtube_cookies", youtubeCookies, "generated_lines", len(lines)-2, "skipped", skipped)

	return strings.Join(lines, "\n")
}

func cookiesHeader() string {
	return "# Netscape HTTP Cookie File\n# This is a generated file! Do not edit.\n"
}

func persistNetscapeCookies(ctx context.Context, q *db.Queries, encMgr *encryption.Manager, userID pgtype.UUID, cookiesContent string) {
	cookiesContent = strings.TrimSpace(cookiesContent)
	if cookiesContent == "" {
		return
	}

	normalizedContent := strings.ReplaceAll(cookiesContent, "\r\n", "\n")
	normalizedContent = strings.ReplaceAll(normalizedContent, "\r", "\n")
	lines := strings.Split(normalizedContent, "\n")

	validCount := 0
	invalidCount := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		parts := strings.Split(trimmed, "\t")
		if len(parts) < 7 {
			invalidCount++
			continue
		}
		expiration, err := strconv.ParseInt(parts[4], 10, 64)
		if err != nil {
			invalidCount++
			continue
		}

		encryptedValue, err := encryption.Encrypt(encMgr, parts[6])
		if err != nil {
			invalidCount++
			continue
		}

		if err := q.InsertCookie(ctx, &db.InsertCookieParams{
			UserID:     userID,
			Domain:     parts[0],
			Flag:       parts[1],
			Path:       parts[2],
			Secure:     parts[3],
			Expiration: expiration,
			Name:       parts[5],
			Value:      encryptedValue,
		}); err != nil {
			invalidCount++
			continue
		}
		validCount++
	}

	if validCount > 0 {
		slog.Info("persisted updated cookies", "user_id", userID, "valid", validCount, "invalid", invalidCount)
	}
}

func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return fmtUUID(u.Bytes)
}

func fmtUUID(b [16]byte) string {
	return strings.ToLower(
		fmt.Sprintf(
			"%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
			b[0], b[1], b[2], b[3],
			b[4], b[5],
			b[6], b[7],
			b[8], b[9],
			b[10], b[11], b[12], b[13], b[14], b[15],
		),
	)
}
