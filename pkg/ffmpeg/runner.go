package ffmpeg

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Process represents a running ffmpeg process with lifecycle management.
type Process struct {
	cmd      *exec.Cmd
	pid      int
	done     chan struct{}
	err      error
	stderr   bytes.Buffer
	progress chan<- Progress
}

// PID returns the process ID, or 0 if not started.
func (p *Process) PID() int {
	return p.pid
}

// Wait blocks until the process completes and returns any error.
func (p *Process) Wait() error {
	<-p.done
	return p.err
}

// Kill sends SIGKILL to the process.
func (p *Process) Kill() error {
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

// Signal sends a signal to the process.
func (p *Process) Signal(sig os.Signal) error {
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Signal(sig)
}

// Done returns a channel that closes when the process exits.
func (p *Process) Done() <-chan struct{} {
	return p.done
}

// Stderr returns the captured stderr output (available after Wait).
func (p *Process) Stderr() string {
	return p.stderr.String()
}

// Start starts an ffmpeg process and returns a Process handle for lifecycle management.
// The caller is responsible for calling Wait() or Kill() to clean up.
func Start(ctx context.Context, args []string, progress chan<- Progress) (*Process, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	p := &Process{
		cmd:      cmd,
		done:     make(chan struct{}),
		progress: progress,
	}

	cmd.Stderr = &p.stderr

	if progress != nil {
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, fmt.Errorf("ffmpeg: failed to create stdout pipe: %w", err)
		}

		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("ffmpeg: failed to start: %w", err)
		}

		p.pid = cmd.Process.Pid

		// Parse progress in background
		go func() {
			defer close(p.done)

			// Read progress
			scanner := bufio.NewScanner(stdout)
			ParseProgressOutput(scanner, progress)

			// Wait for process to exit
			p.err = cmd.Wait()
			if p.err != nil {
				p.err = &Error{
					Args:   args,
					Stderr: p.stderr.String(),
					Err:    p.err,
				}
			}
			close(progress)
		}()
	} else {
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("ffmpeg: failed to start: %w", err)
		}

		p.pid = cmd.Process.Pid

		// Wait in background
		go func() {
			defer close(p.done)
			p.err = cmd.Wait()
			if p.err != nil {
				p.err = &Error{
					Args:   args,
					Stderr: p.stderr.String(),
					Err:    p.err,
				}
			}
		}()
	}

	return p, nil
}

// run executes ffmpeg and waits for completion.
// This is the simple "fire and wait" path.
func run(ctx context.Context, args []string, progress chan<- Progress) error {
	proc, err := Start(ctx, args, progress)
	if err != nil {
		return err
	}
	return proc.Wait()
}

// RunResult contains the outcome of an ffmpeg invocation, including captured stderr.
type RunResult struct {
	// Logs contains the full ffmpeg stderr output (codec info, encoding stats, warnings, etc.).
	// Available regardless of success or failure.
	Logs string
	// Err is non-nil when ffmpeg exited with a non-zero status.
	Err error
}

// runCapture executes ffmpeg, waits for completion, and returns stderr output.
func runCapture(ctx context.Context, args []string) RunResult {
	proc, err := Start(ctx, args, nil)
	if err != nil {
		return RunResult{Err: err}
	}
	waitErr := proc.Wait()
	return RunResult{
		Logs: proc.Stderr(),
		Err:  waitErr,
	}
}

// Error represents an ffmpeg execution error with context.
type Error struct {
	Args   []string
	Stderr string
	Err    error
}

// Error implements error.
func (e *Error) Error() string {
	// Extract just the last few lines of stderr for the error message
	lines := strings.Split(strings.TrimSpace(e.Stderr), "\n")
	var lastLines string
	if len(lines) > 3 {
		lastLines = strings.Join(lines[len(lines)-3:], "\n")
	} else {
		lastLines = strings.Join(lines, "\n")
	}

	if lastLines != "" {
		return fmt.Sprintf("ffmpeg: %v: %s", e.Err, lastLines)
	}
	return fmt.Sprintf("ffmpeg: %v", e.Err)
}

// Unwrap returns the underlying error.
func (e *Error) Unwrap() error {
	return e.Err
}

// FullStderr returns the complete stderr output.
func (e *Error) FullStderr() string {
	return e.Stderr
}

// Command returns the command that was executed.
func (e *Error) Command() string {
	return "ffmpeg " + strings.Join(e.Args, " ")
}
