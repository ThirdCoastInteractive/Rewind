package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

// streamWriter wraps an io.Writer and calls a callback for each line.
type streamWriter struct {
	stream   string
	callback func(stream string, line string)
	buffer   *bytes.Buffer
	pending  []byte
}

func (w *streamWriter) Write(p []byte) (n int, err error) {
	// Also write to buffer for later retrieval
	if w.buffer != nil {
		w.buffer.Write(p)
	}

	// Append to pending data
	w.pending = append(w.pending, p...)

	// Process complete lines.
	// yt-dlp progress output often uses carriage returns (\r) to update the same
	// console line. When we're logging, treat both \n and \r as line boundaries.
	for {
		idx := bytes.IndexAny(w.pending, "\r\n")
		if idx < 0 {
			break
		}

		line := string(w.pending[:idx])

		// Consume delimiter(s). If this is a CRLF sequence, consume both.
		consume := 1
		if w.pending[idx] == '\r' && idx+1 < len(w.pending) && w.pending[idx+1] == '\n' {
			consume = 2
		}
		w.pending = w.pending[idx+consume:]

		if w.callback != nil {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				w.callback(w.stream, trimmed)
			}
		}
	}

	return len(p), nil
}

type ExecError struct {
	Cmd      string
	Args     []string
	ExitCode int
	Stdout   string
	Stderr   string
	Cause    error
}

func (e *ExecError) Error() string {
	cmdline := strings.TrimSpace(e.Cmd + " " + strings.Join(e.Args, " "))
	if e.ExitCode != 0 {
		return fmt.Sprintf("ytdlp: command failed (exit %d): %s", e.ExitCode, cmdline)
	}
	return fmt.Sprintf("ytdlp: command failed: %s", cmdline)
}

func (e *ExecError) Unwrap() error { return e.Cause }

type Client struct {
	// Path to yt-dlp executable. Defaults to "yt-dlp" (PATH lookup).
	Path string

	// EnableCookieJar forces yt-dlp to use a cookie file even if Cookies is empty.
	// This allows extractors (like YouTube) to write updated cookies that can be persisted.
	EnableCookieJar bool

	// Cookies is the cookies.txt content for authentication.
	// If set, a temporary cookies file will be created for each command.
	Cookies string

	// UpdatedCookies is populated with the latest cookies file content when yt-dlp
	// modifies the cookie jar during a command.
	UpdatedCookies string

	// ExtraArgs are always appended before per-call args.
	ExtraArgs []string

	// LastPID is the process ID of the most recently executed command.
	// Only populated after exec() is called.
	LastPID int

	// LogCallback is called for each line of stdout/stderr output.
	// If nil, output is buffered in memory.
	LogCallback func(stream string, line string)

	execFn func(ctx context.Context, name string, args ...string) (stdout []byte, stderr []byte, err error)
}

func New() *Client {
	return &Client{Path: "yt-dlp"}
}

func (c *Client) exec(ctx context.Context, args ...string) (stdout []byte, stderr []byte, err error) {
	name := c.Path
	if strings.TrimSpace(name) == "" {
		name = "yt-dlp"
	}

	// Reset per-exec state.
	c.LastPID = 0

	fullArgs := make([]string, 0, len(c.ExtraArgs)+len(args)+2)
	fullArgs = append(fullArgs, c.ExtraArgs...)
	if c.LogCallback != nil {
		// Force newline progress output so logs are readable.
		// This is a no-op for commands that don't emit progress.
		fullArgs = append(fullArgs, "--newline")
	}

	// Create temporary cookies file if content is provided
	var cookiesFile string
	if c.EnableCookieJar || c.Cookies != "" {
		cookiesFile, err = createTempCookiesFile(c.Cookies)
		if err != nil {
			slog.Error("CRITICAL: failed to create temp cookies file", "error", err)
			return nil, nil, fmt.Errorf("failed to create temp cookies file: %w", err)
		}
		fullArgs = append(fullArgs, "--cookies", cookiesFile)
		slog.Info("ytdlp: Using cookies file", "path", cookiesFile, "size_bytes", len(c.Cookies))
	}

	fullArgs = append(fullArgs, args...)

	if c.execFn != nil {
		if cookiesFile != "" {
			defer os.Remove(cookiesFile)
		}
		return c.execFn(ctx, name, fullArgs...)
	}

	slog.Info("ytdlp: Executing command", "cmd", name, "args", fullArgs)
	cmd := exec.CommandContext(ctx, name, fullArgs...)
	var outBuf, errBuf bytes.Buffer

	// If LogCallback is set, stream output line-by-line
	if c.LogCallback != nil {
		cmd.Stdout = &streamWriter{stream: "stdout", callback: c.LogCallback, buffer: &outBuf}
		cmd.Stderr = &streamWriter{stream: "stderr", callback: c.LogCallback, buffer: &errBuf}
	} else {
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf
	}

	err = cmd.Start()
	if err != nil {
		if cookiesFile != "" {
			_ = os.Remove(cookiesFile)
		}
		return nil, nil, err
	}

	// Store the PID
	c.LastPID = cmd.Process.Pid

	err = cmd.Wait()

	// If a cookies file was used, read it back and keep it if it changed.
	if cookiesFile != "" {
		updated, readErr := os.ReadFile(cookiesFile)
		_ = os.Remove(cookiesFile)
		if readErr != nil {
			slog.Warn("ytdlp: failed to read updated cookies file", "path", cookiesFile, "error", readErr)
		} else {
			updatedStr := string(updated)
			if strings.TrimSpace(updatedStr) != strings.TrimSpace(c.Cookies) {
				c.Cookies = updatedStr
				c.UpdatedCookies = updatedStr
			}
		}
	}

	return outBuf.Bytes(), errBuf.Bytes(), err
}

// Version returns `yt-dlp --version`.
func (c *Client) Version(ctx context.Context) (string, error) {
	stdout, stderr, err := c.exec(ctx, "--version")
	if err != nil {
		return "", wrapExecError("yt-dlp", append([]string{"--version"}, c.ExtraArgs...), stdout, stderr, err)
	}
	return strings.TrimSpace(string(stdout)), nil
}

// Info is a light wrapper over yt-dlp JSON output. It intentionally models only common fields.
// The full JSON is preserved in Raw.
type Info struct {
	ID           string            `json:"id"`
	Title        string            `json:"title"`
	WebpageURL   string            `json:"webpage_url"`
	Extractor    string            `json:"extractor"`
	ExtractorKey string            `json:"extractor_key"`
	Uploader     string            `json:"uploader"`
	Duration     float64           `json:"duration"`
	Entries      []json.RawMessage `json:"entries,omitempty"`
	Raw          json.RawMessage   `json:"-"`
}

// GetInfo runs yt-dlp in "metadata only" mode and parses its JSON output.
// It uses: --dump-single-json --skip-download
func (c *Client) GetInfo(ctx context.Context, url string, extraArgs ...string) (*Info, error) {
	if strings.TrimSpace(url) == "" {
		return nil, fmt.Errorf("ytdlp: url is required")
	}

	args := []string{"--dump-single-json", "--skip-download"}
	args = append(args, extraArgs...)
	args = append(args, url)

	stdout, stderr, err := c.exec(ctx, args...)
	if err != nil {
		return nil, wrapExecError(c.PathOrDefault(), args, stdout, stderr, err)
	}

	raw := bytes.TrimSpace(stdout)
	info := &Info{Raw: append([]byte(nil), raw...)}
	if err := json.Unmarshal(raw, info); err != nil {
		return nil, fmt.Errorf("ytdlp: parse json: %w", err)
	}

	return info, nil
}

// PathOrDefault returns the configured path or "yt-dlp" if unset.
func (c *Client) PathOrDefault() string {
	if strings.TrimSpace(c.Path) == "" {
		return "yt-dlp"
	}
	return c.Path
}

// Update runs `yt-dlp -U` to update to the latest version.
func (c *Client) Update(ctx context.Context, extraArgs ...string) error {
	args := []string{"-U"}
	args = append(args, extraArgs...)

	stdout, stderr, err := c.exec(ctx, args...)
	if err != nil {
		return wrapExecError(c.PathOrDefault(), args, stdout, stderr, err)
	}
	return nil
}

func wrapExecError(cmd string, args []string, stdout []byte, stderr []byte, cause error) error {
	exitCode := 0
	var ee *exec.ExitError
	if errors.As(cause, &ee) {
		exitCode = ee.ExitCode()
	}

	return &ExecError{
		Cmd:      cmd,
		Args:     args,
		ExitCode: exitCode,
		Stdout:   strings.TrimSpace(string(stdout)),
		Stderr:   strings.TrimSpace(string(stderr)),
		Cause:    cause,
	}
}

// createTempCookiesFile creates a temporary file with the cookies content
func createTempCookiesFile(content string) (string, error) {
	tmpFile, err := os.CreateTemp("", "ytdlp-cookies-*.txt")
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()

	if _, err := tmpFile.WriteString(content); err != nil {
		os.Remove(tmpFile.Name())
		return "", err
	}

	return tmpFile.Name(), nil
}
