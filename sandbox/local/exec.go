package local

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/taichu-lang/raven-agents/sandbox"
)

// DefaultMaxOutputBytes caps the output captured into an ExecResult when the request sets no limit of its own
// (10 MiB).
const DefaultMaxOutputBytes = 10 << 20

// Exec runs a shell command as a local child process and blocks until it finishes. A non-zero exit code is
// reported in the result, not as an error.
//
// TTY execution is not implemented yet: it returns sandbox.ErrNotSupported.
func (s *Sandbox) Exec(ctx context.Context, req *sandbox.ExecRequest) (*sandbox.ExecResult, error) {
	if req == nil {
		return nil, errors.New("exec request is required")
	}
	if strings.TrimSpace(req.Command) == "" {
		return nil, errors.New("exec command is required")
	}
	if req.TTY {
		// TODO(Leo): allocate a PTY for one-shot TTY execution. Requires a cross-platform pty library since
		// the agent targets linux, macOS and windows: creack/pty has no windows support, so evaluate
		// aymanbagabas/go-pty or charmbracelet/x/pty (both wrap ConPTY).
		return nil, fmt.Errorf("%w: tty execution", sandbox.ErrNotSupported)
	}

	runCtx, release, err := s.beginRun(ctx, req.Timeout)
	if err != nil {
		return nil, err
	}
	defer release()

	shell := s.shell(req.Shell)
	// Executing arbitrary commands is the purpose of a sandbox.
	cmd := exec.CommandContext(runCtx, shell, shellArgs(shell, req.Command)...) // #nosec G204
	cmd.Dir = s.workingDir(req.WorkingDir)
	cmd.Env = s.env(req.Env)
	cmd.Stdin = req.Stdin
	// Cancellation kills the whole process group (see proc_*.go), so children forked by the shell do not
	// survive it. WaitDelay is the safety net for survivors that escaped the group: after the grace period the
	// pipes are closed forcibly instead of hanging Wait forever.
	cmd.SysProcAttr = newSysProcAttr()
	cmd.Cancel = func() error { return killCommand(cmd) }
	cmd.WaitDelay = 5 * time.Second

	capture := newOutputCapture(req.Output, req.MaxOutputBytes, runCtx)
	cmd.Stdout = &streamWriter{capture: capture, stream: sandbox.StreamStdout}
	cmd.Stderr = &streamWriter{capture: capture, stream: sandbox.StreamStderr}

	s.logger.Debug("exec start", "command", req.Command, "working_dir", cmd.Dir)

	startErr := cmd.Start()
	if startErr != nil {
		return nil, fmt.Errorf("start command: %w", startErr)
	}

	waitErr := cmd.Wait()
	return s.buildResult(ctx, runCtx, waitErr, capture)
}

// beginRun derives the execution context (applying the request timeout, then the sandbox default) and registers
// its cancel function so that Sandbox.Close can kill the command. It returns ErrClosed when the sandbox is
// already closed.
func (s *Sandbox) beginRun(
	parent context.Context,
	timeout time.Duration,
) (context.Context, context.CancelFunc, error) {
	if timeout <= 0 {
		timeout = s.spec.Limits.ProcessTimeout
	}

	var runCtx context.Context
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(parent, timeout)
	} else {
		runCtx, cancel = context.WithCancel(parent)
	}

	// The closed check happens under the same lock Close uses to set the flag and snapshot the cancels, so a
	// command can not be registered after Close has already collected the running ones.
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		cancel()
		return nil, nil, ErrClosed
	}
	s.nextRunID++
	runID := s.nextRunID
	s.runs[runID] = cancel
	s.wg.Add(1)
	s.mu.Unlock()

	release := func() {
		s.mu.Lock()
		delete(s.runs, runID)
		s.mu.Unlock()
		s.wg.Done()
		cancel()
	}
	return runCtx, release, nil
}

// shellArgs builds the argument list that runs a command line through the given shell binary: "-c" for unix
// shells, and the native one-shot switch for the windows shells.
func shellArgs(shell, command string) []string {
	// Split on both separators so that a windows-style shell path is also recognized when the agent itself runs
	// on another platform, ex: in tests.
	base := strings.ToLower(shell)
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	switch {
	case strings.HasPrefix(base, "powershell"), strings.HasPrefix(base, "pwsh"):
		return []string{"-NoProfile", "-Command", command}
	case strings.HasPrefix(base, "cmd"):
		return []string{"/c", command}
	default:
		return []string{"-c", command}
	}
}

// buildResult turns the wait outcome into an ExecResult. A caller-cancelled context is returned as an error; a
// timeout or a kill by Sandbox.Close is reported in the result with ExitCode -1.
func (s *Sandbox) buildResult(
	parent context.Context,
	runCtx context.Context,
	waitErr error,
	capture *outputCapture,
) (*sandbox.ExecResult, error) {
	result := &sandbox.ExecResult{
		ExitCode:  0,
		Stdout:    capture.stdout(),
		Stderr:    capture.stderr(),
		Truncated: capture.truncated(),
	}

	if parent.Err() != nil {
		return nil, parent.Err()
	}

	timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded)
	if runCtx.Err() != nil && !timedOut {
		// Cancelled by Sandbox.Close; the command was killed before exiting.
		result.ExitCode = -1
		return result, nil
	}

	result.TimedOut = timedOut

	var exitErr *exec.ExitError
	switch {
	case waitErr == nil:
		result.ExitCode = 0
	case errors.As(waitErr, &exitErr):
		if timedOut {
			result.ExitCode = -1
		} else {
			result.ExitCode = exitErr.ExitCode()
		}
	default:
		return nil, fmt.Errorf("wait for command: %w", waitErr)
	}

	s.logger.Debug("exec finished", "exit_code", result.ExitCode, "timed_out", result.TimedOut)
	return result, nil
}

// shell resolves the shell binary for a request, falling back to the sandbox default.
func (s *Sandbox) shell(requested string) string {
	if requested != "" {
		return requested
	}
	return s.defaultShell
}

// workingDir resolves the effective working directory: the request override, then the sandbox default, then the
// provider process's own directory.
func (s *Sandbox) workingDir(requested string) string {
	if requested != "" {
		return requested
	}
	return s.spec.WorkingDir
}

// env composes the child environment: the provider process environment, then the sandbox baseline, then the
// per-request overrides.
func (s *Sandbox) env(overrides map[string]string) []string {
	merged := make(map[string]string)
	for _, entry := range os.Environ() {
		if key, value, ok := strings.Cut(entry, "="); ok {
			merged[key] = value
		}
	}
	maps.Copy(merged, s.spec.Env)
	maps.Copy(merged, overrides)

	env := make([]string, 0, len(merged))
	for key, value := range merged {
		env = append(env, key+"="+value)
	}
	return env
}

// outputCapture accumulates command output up to a byte limit while forwarding every chunk to the caller's
// streaming channel. The limit is shared across stdout and stderr because both end up in the same result.
type outputCapture struct {
	mu          sync.Mutex
	stdoutBuf   bytes.Buffer
	stderrBuf   bytes.Buffer
	limit       int
	written     int
	isTruncated bool
	stream      chan<- sandbox.OutputEvent
	streamCtx   context.Context
}

func newOutputCapture(
	stream chan<- sandbox.OutputEvent,
	maxOutputBytes int,
	ctx context.Context,
) *outputCapture {
	if maxOutputBytes <= 0 {
		maxOutputBytes = DefaultMaxOutputBytes
	}
	return &outputCapture{limit: maxOutputBytes, stream: stream, streamCtx: ctx}
}

// append records a chunk for one stream, dropping whatever exceeds the shared capture limit, and forwards the
// full chunk to the streaming channel. The channel send never blocks a killed command: it gives up when the
// execution context is done.
func (c *outputCapture) append(stream sandbox.Stream, data []byte) {
	if c.stream != nil && len(data) > 0 {
		event := sandbox.OutputEvent{Stream: stream, Data: append([]byte(nil), data...)}
		select {
		case c.stream <- event:
		case <-c.streamCtx.Done():
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	remaining := c.limit - c.written
	if remaining <= 0 {
		c.isTruncated = true
		return
	}
	if len(data) > remaining {
		data = data[:remaining]
		c.isTruncated = true
	}
	c.written += len(data)

	if stream == sandbox.StreamStderr {
		c.stderrBuf.Write(data)
		return
	}
	c.stdoutBuf.Write(data)
}

func (c *outputCapture) stdout() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stdoutBuf.String()
}

func (c *outputCapture) stderr() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stderrBuf.String()
}

func (c *outputCapture) truncated() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.isTruncated
}

// streamWriter adapts outputCapture to io.Writer for one output stream.
type streamWriter struct {
	capture *outputCapture
	stream  sandbox.Stream
}

var _ io.Writer = (*streamWriter)(nil)

func (w *streamWriter) Write(p []byte) (int, error) {
	w.capture.append(w.stream, p)
	return len(p), nil
}
