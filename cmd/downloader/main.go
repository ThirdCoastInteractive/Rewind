package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/encryption"
	"thirdcoast.systems/rewind/pkg/utils/crypto"
	"thirdcoast.systems/rewind/pkg/ytdlp"
)

// generateCookiesFile generates a Netscape format cookies file from database cookies
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

		// Decrypt the cookie value
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

	// Normalize line endings (handle Windows CRLF, Unix LF, old Mac CR)
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

		// Netscape format: domain\tflag\tpath\tsecure\texpiration\tname\tvalue
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

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("Starting downloader service")

	conf, err := config.LoadConfig(ctx)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	if conf.DatabaseRetries <= 0 {
		conf.DatabaseRetries = 10
	}

	spoolDir := strings.TrimSpace(os.Getenv("SPOOL_DIR"))
	if spoolDir == "" {
		spoolDir = "/spool"
	}
	if err := os.MkdirAll(filepath.Join(spoolDir, "downloads"), 0o755); err != nil {
		slog.Error("failed to create spool dir", "dir", spoolDir, "error", err)
		os.Exit(1)
	}

	ytdlpUpdateCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	err = ytdlp.New().Update(ytdlpUpdateCtx)
	if err != nil {
		slog.Warn("failed to update yt-dlp", "error", err)
	} else {
		slog.Info("yt-dlp updated successfully")
		cancel()
	}

	pool, err := application.OpenDBPoolWithRetry(ctx, *conf)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Initialize encryption manager
	encMgr, err := application.InitEncryptionManager()
	if err != nil {
		slog.Error("failed to initialize encryption manager", "error", err)
		os.Exit(1)
	}

	dbc, err := db.NewDatabaseConnection(ctx, pool)
	if err != nil {
		slog.Error("failed to create database connection", "error", err)
		os.Exit(1)
	}
	defer dbc.Close()

	// Recover orphaned jobs stuck in "processing" from previous crashes/restarts
	slog.Info("Recovering stuck download jobs from previous service instances")
	if err := dbc.Queries(ctx).RecoverStuckDownloadJobs(ctx); err != nil {
		slog.Error("failed to recover stuck download jobs", "error", err)
		// Non-fatal - continue startup
	}

	workers := envInt("DOWNLOAD_WORKERS", 2)
	client := ytdlp.New()
	client.Path = "/usr/local/bin/yt-dlp"

	wake := make(chan struct{}, 1)
	go listenAndSignal(ctx, conf.DatabaseDSN, "download_jobs", wake)

	slog.Info("Downloader workers started", "workers", workers)
	for i := 0; i < workers; i++ {
		go downloadWorker(ctx, dbc, client, spoolDir, encMgr, wake)
	}

	<-ctx.Done()
	slog.Info("Downloader service stopping")
}

func downloadWorker(ctx context.Context, dbc *db.DatabaseConnection, client *ytdlp.Client, spoolDir string, encMgr *encryption.Manager, wake <-chan struct{}) {
	q := dbc.Queries(ctx)
	for {
		if ctx.Err() != nil {
			return
		}

		// Drain as many jobs as we can
		for {
			job, err := q.DequeueDownloadJob(ctx)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					break
				}
				slog.Error("failed to dequeue download job", "error", err)
				time.Sleep(2 * time.Second)
				break
			}

			// Create a fresh client for this job (with its own cookies)
			jobClient := ytdlp.New()
			jobClient.Path = client.Path
			jobClient.ExtraArgs = client.ExtraArgs

			if err := processDownloadJob(ctx, q, jobClient, spoolDir, encMgr, job); err != nil {
				jobID := uuidString(job.ID)

				// Log detailed error information
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
			// new job notification
		case <-time.After(5 * time.Second):
			// periodic poll
		}
	}
}

func processDownloadJob(ctx context.Context, q *db.Queries, client *ytdlp.Client, spoolDir string, encMgr *encryption.Manager, job *db.DownloadJob) error {
	jobID := uuidString(job.ID)
	if jobID == "" {
		return errors.New("invalid job id")
	}

	// Always enable a cookie jar so extractors can write updated cookies.
	client.EnableCookieJar = true
	defer func() {
		if strings.TrimSpace(client.UpdatedCookies) == "" {
			return
		}
		persistCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		persistNetscapeCookies(persistCtx, q, encMgr, job.ArchivedBy, client.UpdatedCookies)
	}()

	// Set up log callback to store output in database
	client.LogCallback = func(stream string, line string) {
		if err := q.InsertYtdlpLog(ctx, &db.InsertYtdlpLogParams{
			JobID:   job.ID,
			Stream:  db.LogStream(stream),
			Message: line,
		}); err != nil {
			slog.Warn("failed to insert ytdlp log", "job_id", jobID, "error", err)
		}
	}
	defer func() {
		client.LogCallback = nil
	}()

	// Get user's cookies from database and generate Netscape format
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

		// Debug: Log first few lines and last few lines of cookies
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

	destDir := filepath.Join(spoolDir, "downloads", jobID)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}

	var infoPath string
	if job.Refresh {
		infoPath = filepath.Join(destDir, "refresh.info.json")
		slog.Info("Refreshing metadata", "job_id", jobID, "url", job.URL)
		if err := client.DumpInfoJSON(ctx, job.URL, infoPath, "--no-playlist"); err != nil {
			return err
		}

		// Best-effort: refresh thumbnails and subtitles/captions too.
		if err := client.WriteThumbnail(ctx, job.URL, destDir); err != nil {
			var execErr *ytdlp.ExecError
			if errors.As(err, &execErr) {
				slog.Warn("failed to fetch thumbnail", "job_id", jobID, "error", err, "stderr", execErr.Stderr)
			} else {
				slog.Warn("failed to fetch thumbnail", "job_id", jobID, "error", err)
			}
		}
		if err := client.WriteSubtitles(ctx, job.URL, destDir); err != nil {
			var execErr *ytdlp.ExecError
			if errors.As(err, &execErr) {
				slog.Warn("failed to fetch subtitles", "job_id", jobID, "error", err, "stderr", execErr.Stderr)
			} else {
				slog.Warn("failed to fetch subtitles", "job_id", jobID, "error", err)
			}
		}

		// Store PID after command starts
		if client.LastPID > 0 {
			lastPID := int64(client.LastPID)
			_ = q.UpdateDownloadJobPID(ctx, &db.UpdateDownloadJobPIDParams{ID: job.ID, ProcessPid: &lastPID})
		}
	} else {
		slog.Info("Downloading", "job_id", jobID, "url", job.URL)
		downloadArgs := []string{"--no-playlist"}
		if len(job.ExtraArgs) > 0 {
			downloadArgs = append(downloadArgs, job.ExtraArgs...)
		}
		if err := client.Download(ctx, job.URL, destDir, downloadArgs...); err != nil {
			return err
		}

		infoMatches, err := filepath.Glob(filepath.Join(destDir, "*.info.json"))
		if err != nil {
			return err
		}
		if len(infoMatches) == 0 {
			return errors.New("yt-dlp did not produce .info.json")
		}
		infoPath = infoMatches[0]

		// Store PID after command starts
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

func listenAndSignal(ctx context.Context, dsn string, channel string, signalCh chan<- struct{}) {
	for {
		if ctx.Err() != nil {
			return
		}

		// Parse using pgxpool so pool_* DSN params are consumed client-side
		// (otherwise they get forwarded to Postgres as startup params and cause FATAL).
		poolConf, err := pgxpool.ParseConfig(dsn)
		if err != nil {
			slog.Error("listen parse config failed", "channel", channel, "error", err)
			time.Sleep(2 * time.Second)
			continue
		}

		conn, err := pgx.ConnectConfig(ctx, poolConf.ConnConfig)
		if err != nil {
			slog.Error("listen connect failed", "channel", channel, "error", err)
			time.Sleep(2 * time.Second)
			continue
		}

		q := db.New(conn)
		switch channel {
		case "download_jobs":
			err = q.ListenDownloadJobs(ctx)
		default:
			err = fmt.Errorf("unsupported listen channel: %s", channel)
		}
		if err != nil {
			slog.Error("LISTEN failed", "channel", channel, "error", err)
			_ = conn.Close(ctx)
			time.Sleep(2 * time.Second)
			continue
		}

		for {
			if ctx.Err() != nil {
				_ = conn.Close(ctx)
				return
			}

			err := conn.PgConn().WaitForNotification(ctx)
			if err != nil {
				slog.Error("wait for notification failed", "channel", channel, "error", err)
				_ = conn.Close(ctx)
				break
			}

			select {
			case signalCh <- struct{}{}:
			default:
			}
		}
	}
}

func envInt(name string, def int) int {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmtUUID(b)
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
