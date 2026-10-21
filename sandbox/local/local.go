// Package local implements sandbox.Provider by executing commands directly on the host as child processes. It
// is the desktop / development backend: there is no isolation beyond the operating system process boundary, so
// untrusted code should run on a container-based backend instead.
//
// The implementation is cross-platform (linux, macOS and windows) and relies on os/exec only. Spec.Image is
// ignored; dependencies must already be installed on the host. Resource limits other than ProcessTimeout are
// best-effort and currently not enforced.
package local

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"sync"

	"github.com/google/uuid"
	"github.com/taichu-lang/raven-agents/sandbox"
)

// Kind is the provider name used by configuration to select this backend.
const Kind = "local"

// ErrClosed is returned when a sandbox is used after it has been closed.
var ErrClosed = errors.New("sandbox is closed")

var (
	_ sandbox.Provider = (*Provider)(nil)
	_ sandbox.Sandbox  = (*Sandbox)(nil)
)

// Provider creates sandboxes that run commands as local child processes.
type Provider struct {
	mu     sync.Mutex
	closed bool

	// sandboxes tracks live handles by ID so that Destroy can tear one down without a handle, ex: after the
	// owner crashed.
	sandboxes map[string]*Sandbox

	logger *slog.Logger
}

// NewProvider creates a local provider.
func NewProvider() *Provider {
	return &Provider{
		sandboxes: make(map[string]*Sandbox),
		logger:    slog.With("provider", Kind),
	}
}

func (p *Provider) Kind() string {
	return Kind
}

// Create registers a new local sandbox. Spec.Image is ignored; Spec.WorkingDir must exist when set. An empty
// Spec.ID is filled with a generated UUID, and a duplicate ID is rejected.
func (p *Provider) Create(_ context.Context, spec *sandbox.Spec) (sandbox.Sandbox, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil, ErrClosed
	}

	if spec == nil {
		spec = &sandbox.Spec{}
	}

	id := spec.ID
	if id == "" {
		id = uuid.NewString()
	}

	if _, exists := p.sandboxes[id]; exists {
		return nil, fmt.Errorf("sandbox %q already exists", id)
	}

	if spec.WorkingDir != "" {
		info, err := os.Stat(spec.WorkingDir)
		if err != nil {
			return nil, fmt.Errorf("sandbox working dir: %w", err)
		}

		if !info.IsDir() {
			return nil, fmt.Errorf("sandbox working dir %q is not a directory", spec.WorkingDir)
		}
	}

	sb := &Sandbox{
		id:           id,
		spec:         *spec,
		defaultShell: defaultShell(),
		runs:         make(map[int64]context.CancelFunc),
		// The sandbox stays unaware of the provider; deregistration is injected as a plain callback to keep the
		// dependency one-directional.
		onClose: func() { p.remove(id) },
		logger:  p.logger.With("sandbox_id", id),
	}
	sb.spec.ID = id
	p.sandboxes[id] = sb

	sb.logger.Debug("sandbox created", "working_dir", spec.WorkingDir, "shell", sb.defaultShell)
	return sb, nil
}

// Destroy closes the sandbox registered under the ID, if any, and removes it from the registry.
func (p *Provider) Destroy(ctx context.Context, id string) error {
	p.mu.Lock()
	sb, ok := p.sandboxes[id]
	if !ok {
		p.mu.Unlock()
		return fmt.Errorf("%w: %s", sandbox.ErrNotFound, id)
	}
	delete(p.sandboxes, id)
	p.mu.Unlock()

	// Close runs without the registry lock: it waits for running commands and its onClose callback re-acquires
	// the lock.
	return sb.Close(ctx)
}

// Close marks the provider closed so that no new sandboxes can be created. Sandboxes already handed out are not
// closed by this call.
func (p *Provider) Close(_ context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.closed = true
	return nil
}

// remove drops a sandbox from the registry when its handle is closed directly.
func (p *Provider) remove(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.sandboxes, id)
}

// Sandbox executes commands as local child processes. It is safe for concurrent use: each Exec spawns an
// independent process, and Close cancels all of them.
type Sandbox struct {
	id           string
	logger       *slog.Logger
	spec         sandbox.Spec
	defaultShell string

	// onClose, when set, is invoked once after the sandbox has closed, ex: to deregister it from the creator's
	// registry.
	onClose func()
	closed  bool

	// runs holds the cancel function of every running command, keyed by an per-sandbox run ID, so that Close
	// can terminate them; wg waits for the running Exec calls on Close.
	runs      map[int64]context.CancelFunc
	nextRunID int64
	mu        sync.Mutex
	wg        sync.WaitGroup
}

func (s *Sandbox) ID() string {
	return s.id
}

func (s *Sandbox) Capabilities() sandbox.Capabilities {
	// A local sandbox shares the host's network stack.
	return sandbox.Capabilities{Network: true}
}

// Close cancels all running commands, waits for them to finish and removes the sandbox from the provider
// registry. It is idempotent.
func (s *Sandbox) Close(_ context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}

	s.closed = true
	cancels := make([]context.CancelFunc, 0, len(s.runs))
	for _, cancel := range s.runs {
		cancels = append(cancels, cancel)
	}
	s.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
	s.wg.Wait()

	if s.onClose != nil {
		s.onClose()
	}

	s.logger.Debug("sandbox closed")
	return nil
}

// defaultShell resolves the shell used when a request names none: the user's default shell on unix, and
// PowerShell on windows where there is no $SHELL.
func defaultShell() string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}

	if runtime.GOOS == "windows" {
		return "powershell"
	}

	return "sh"
}
