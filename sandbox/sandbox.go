// Package sandbox defines the abstract execution environment used by agent tools, starting with the
// exec-command shell tool. A Sandbox is an isolated environment bound to one agent conversation.
//
// Concrete behavior is supplied by Provider implementations chosen per deployment scenario: direct local
// process execution or a CubeSandbox wrapper on a desktop, docker or kubernetes instances on a server. Callers
// depend only on the interfaces declared here and use Capabilities to adapt to what a backend supports.
//
// This is the minimal capability set: one-shot command execution. Interpreter runtimes (python / mjs) need no
// dedicated abstraction since the shell command line already carries the bootstrap, ex: "python3 main.py".
// Interactive terminal (PTY) sessions, stateful code sessions and sandbox filesystem access are deliberately
// left out; they can be added later as new interfaces with matching Capabilities flags without breaking
// existing implementations.
package sandbox

import (
	"context"
	"errors"
	"io"
	"time"
)

// Errors returned by providers and sandboxes. Implementations should wrap these sentinels so that callers can
// match them with errors.Is.
var (
	// ErrNotFound is returned when the referenced sandbox does not exist, ex: a Destroy call for an already
	// removed sandbox.
	ErrNotFound = errors.New("sandbox not found")

	// ErrNotSupported is returned when the backend cannot perform the requested operation; callers should check
	// Capabilities first to avoid it.
	ErrNotSupported = errors.New("operation not supported")
)

// Stream identifies which output stream an OutputEvent belongs to.
type Stream int

const (
	StreamStdout Stream = iota
	StreamStderr
)

// OutputEvent is one chunk of output produced by a running command, delivered as soon as it arrives when the
// request carries an Output channel.
type OutputEvent struct {
	Stream Stream
	Data   []byte
}

// Provider creates and manages sandboxes on one backend. Typical implementations are a local process backend
// for the desktop, a CubeSandbox wrapper, and docker or kubernetes backends for servers.
type Provider interface {
	// Kind returns the backend name, ex: "local", "cubesandbox", "docker" or "kubernetes". Configuration uses
	// it to select the provider.
	Kind() string

	// Create provisions a new sandbox from the spec.
	Create(ctx context.Context, spec *Spec) (Sandbox, error)

	// Destroy tears a sandbox down by ID and deletes everything it holds, whether or not a handle is currently
	// open, ex: garbage collecting sandboxes leaked by a crashed agent process. It returns ErrNotFound when the
	// sandbox is unknown.
	Destroy(ctx context.Context, id string) error

	// Close releases the provider's own resources, ex: client connections to the docker daemon or the
	// kubernetes API. Sandbox handles already handed out are not closed by this call.
	Close(ctx context.Context) error
}

// Spec describes the sandbox to create. Fields a backend cannot honor are ignored; zero values mean backend
// defaults.
type Spec struct {
	// ID is the caller-chosen identifier, ex: the conversation ID, so that the sandbox can be found again
	// later. Empty means the backend generates one.
	ID string

	// Image is the runtime environment for container-based backends, ex: a docker image with python and node
	// preinstalled. This is the primary mechanism for preparing dependencies; local execution ignores it.
	Image string

	// WorkingDir is the default working directory inside the sandbox; relative paths in requests resolve
	// against it. How the directory is provisioned, ex: bind mounting a host workspace into a container, is up
	// to the backend.
	WorkingDir string

	// Env holds baseline environment variables for every execution in the sandbox.
	Env map[string]string

	// Limits constrains the resources the sandbox may consume.
	Limits ResourceLimits
}

// ResourceLimits are best-effort constraints; each backend enforces them as precisely as it can, and zero
// values mean no explicit limit.
type ResourceLimits struct {
	// MemoryBytes caps total memory usage.
	MemoryBytes int64

	// CPUCores caps CPU cores, ex: 2 or 0.5; fractions are honored where possible.
	CPUCores float64

	// DiskBytes caps writable disk space.
	DiskBytes int64

	// ProcessTimeout is the default wall-clock limit for a single command when the request itself sets none.
	ProcessTimeout time.Duration
}

// Sandbox is an isolated execution environment bound to one agent conversation. Tools are invoked concurrently
// by the agent, so implementations must be safe for concurrent use.
//
// Lifecycle: a sandbox is created by a Provider when the conversation starts and closed when it ends. Close
// terminates commands still running in the sandbox and releases the handle.
type Sandbox interface {
	// ID returns the sandbox identifier, unique within its provider.
	ID() string

	// Exec runs a shell command and blocks until it finishes. A non-zero exit code is reported in the result,
	// not as an error; the returned error is only for failures of the execution itself. Output streams to
	// req.Output when it is set, and is always captured into the result up to req.MaxOutputBytes.
	Exec(ctx context.Context, req *ExecRequest) (*ExecResult, error)

	// Capabilities reports what this backend supports, so that tools can degrade gracefully instead of failing
	// with ErrNotSupported.
	Capabilities() Capabilities

	// Close terminates all running commands and releases the sandbox. Whether the underlying environment is
	// destroyed or kept for reuse is up to the backend.
	Close(ctx context.Context) error
}

// Capabilities describes what a backend supports. Not every provider honors every operation: a local backend
// runs whatever is installed on the host, while a hosted backend may refuse PTYs or block the network. Tools
// check these before using an optional feature.
type Capabilities struct {
	// Network reports whether the sandbox can reach the network, which decides if dependencies can be installed
	// at runtime, ex: "pip install" via Exec.
	Network bool
}

// ExecRequest describes one shell command execution.
type ExecRequest struct {
	// Command is the shell command line to run. The interpreter bootstrap is part of the command itself, ex:
	// "python3 main.py" or "node --input-type=module -e '...'"; no separate language selection is needed.
	Command string

	// Shell is the shell binary to launch, ex: "/bin/bash"; empty means the sandbox default.
	Shell string

	// WorkingDir overrides the sandbox default working directory for this command.
	WorkingDir string

	// Env adds environment variables on top of the sandbox baseline.
	Env map[string]string

	// Stdin, when set, is streamed to the command's standard input.
	Stdin io.Reader

	// Timeout is the wall-clock limit for this command; zero falls back to ResourceLimits.ProcessTimeout, then
	// to the backend default. The context deadline always applies as well.
	Timeout time.Duration

	// TTY runs the command on a pseudo terminal when true, for programs that detect a terminal and refuse to
	// run or degrade without one. This is a one-shot PTY, not an interactive session: output streams are merged
	// into Stdout and may contain terminal control sequences.
	TTY bool

	// Output, when set, receives output chunks as they are produced, for streaming long-running commands. The
	// sandbox never closes this channel.
	Output chan<- OutputEvent

	// MaxOutputBytes caps how much output is captured into the result; the excess is dropped and Truncated is
	// set. Zero means the backend default.
	MaxOutputBytes int
}

// ExecResult is the outcome of a finished command.
type ExecResult struct {
	// ExitCode is the exit status of the command, or -1 when it was killed before exiting, ex: by the timeout.
	// A caller-cancelled context is returned as an error instead of a result.
	ExitCode int

	// Stdout and Stderr hold the captured output, limited by MaxOutputBytes. They are merged into Stdout when
	// the request asked for a TTY.
	Stdout string
	Stderr string

	// Truncated reports whether the captured output exceeded the limit.
	Truncated bool

	// TimedOut reports whether the command was killed by the timeout.
	TimedOut bool
}
