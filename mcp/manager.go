// Package mcp bridges the tools of MCP servers to tool.FuncTool. Sessions live in a process wide Manager,
// because every chat request builds a new agent while a stdio server is a child process and an http server a
// long connection; agents only ever see the tool snapshot.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"reflect"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/taichu-lang/raven-agents/tool"
	"golang.org/x/sync/singleflight"
)

// Manager owns the sessions of all servers, keyed by an opaque key chosen by the caller (ex:
// "<userID>/<name>"), so the framework knows nothing about users. Connections are lazy: the first Tools or tool
// call connects, concurrent attempts for one key are merged, and a broken session is re-established once per
// call.
type Manager struct {
	logger      *slog.Logger
	callTimeout time.Duration
	idleTimeout time.Duration

	// dial builds the transport of a config, replaced in tests.
	dial func(cfg *ServerConfig) (mcpsdk.Transport, error)

	mu      sync.Mutex
	servers map[string]*serverState
	closed  bool
	group   singleflight.Group

	stopJanitor chan struct{}
}

// serverState is the session and the tool list of one key. Both are replaced on reconnect.
type serverState struct {
	manager *Manager
	key     string
	cfg     ServerConfig

	mu       sync.Mutex
	session  *mcpsdk.ClientSession
	tools    []*mcpsdk.Tool
	lastUsed time.Time
}

func NewManager() *Manager {
	m := &Manager{
		logger:      slog.With("provider", "mcp"),
		callTimeout: defaultCallTimeout,
		idleTimeout: defaultIdleTimeout,
		servers:     make(map[string]*serverState),
		stopJanitor: make(chan struct{}),
	}
	m.dial = dialTransport

	go m.reclaimIdle()
	return m
}

// Tools connects to the server when needed and returns its tools as function tools named
// mcp__<name>__<tool>. A tool whose schema cannot be converted is skipped with a warning.
func (m *Manager) Tools(ctx context.Context, key string, cfg ServerConfig) ([]tool.FuncTool, error) {
	state, err := m.state(key, cfg)
	if err != nil {
		return nil, err
	}

	if _, err := state.ensure(ctx); err != nil {
		return nil, err
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	tools := make([]tool.FuncTool, 0, len(state.tools))
	for _, def := range state.tools {
		bridged, err := newTool(state, cfg.Name, def)
		if err != nil {
			m.logger.Warn("skip mcp tool", "server", cfg.Name, "tool", def.Name, "err", err)
			continue
		}

		tools = append(tools, bridged)
	}

	return tools, nil
}

// Disconnect closes the session of the key, if any.
func (m *Manager) Disconnect(key string) {
	m.mu.Lock()
	state := m.servers[key]
	delete(m.servers, key)
	m.mu.Unlock()

	if state != nil {
		state.close()
	}
}

// Close closes every session and stops the idle reclaim. It is idempotent.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}

	m.closed = true
	servers := m.servers
	m.servers = make(map[string]*serverState)
	close(m.stopJanitor)
	m.mu.Unlock()

	for _, state := range servers {
		state.close()
	}
}

// state returns the state of the key. A changed config (ex: the user rotated a credential) replaces it, and
// the old session is closed.
func (m *Manager) state(key string, cfg ServerConfig) (*serverState, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, errors.New("mcp: manager is closed")
	}

	if existing := m.servers[key]; existing != nil {
		if reflect.DeepEqual(existing.cfg, cfg) {
			m.mu.Unlock()
			return existing, nil
		}

		delete(m.servers, key)
		defer existing.close()
	}

	state := &serverState{manager: m, key: key, cfg: cfg, lastUsed: time.Now()}
	m.servers[key] = state
	m.mu.Unlock()

	return state, nil
}

// reclaimIdle closes the stdio sessions that have not been used for a while, so child processes do not stay
// around for users who left. They reconnect lazily.
func (m *Manager) reclaimIdle() {
	interval := m.idleTimeout / 2
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopJanitor:
			return
		case <-ticker.C:
		}

		m.mu.Lock()
		states := make([]*serverState, 0, len(m.servers))
		for _, state := range m.servers {
			states = append(states, state)
		}
		m.mu.Unlock()

		for _, state := range states {
			state.closeIfIdle(m.idleTimeout)
		}
	}
}

// ensure returns the live session, connecting first when there is none. Connecting runs in a singleflight
// group, so concurrent callers share one attempt; the attempt is detached from the first caller's context,
// which may be cancelled while others still wait, and bounded by its own timeout instead.
func (s *serverState) ensure(ctx context.Context) (*mcpsdk.ClientSession, error) {
	s.mu.Lock()
	session := s.session
	s.lastUsed = time.Now()
	s.mu.Unlock()

	if session != nil {
		return session, nil
	}

	resultCh := s.manager.group.DoChan(s.key, func() (any, error) {
		return s.connect()
	})

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultCh:
		if result.Err != nil {
			return nil, result.Err
		}

		return result.Val.(*mcpsdk.ClientSession), nil
	}
}

func (s *serverState) connect() (*mcpsdk.ClientSession, error) {
	s.mu.Lock()
	if s.session != nil {
		session := s.session
		s.mu.Unlock()
		return session, nil
	}
	s.mu.Unlock()

	timeout := s.cfg.ConnectTimeout
	if timeout <= 0 {
		timeout = defaultConnectTimeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	transport, err := s.manager.dial(&s.cfg)
	if err != nil {
		return nil, fmt.Errorf("mcp server %q: %w", s.cfg.Name, err)
	}

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "raven-agents", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect mcp server %q: %w", s.cfg.Name, err)
	}

	var tools []*mcpsdk.Tool
	for def, err := range session.Tools(ctx, nil) {
		if err != nil {
			_ = session.Close()
			return nil, fmt.Errorf("list tools of mcp server %q: %w", s.cfg.Name, err)
		}

		tools = append(tools, def)
	}

	s.mu.Lock()
	s.session = session
	s.tools = tools
	s.mu.Unlock()

	s.manager.logger.Info("connected mcp server", "server", s.cfg.Name, "tools", len(tools))

	// Mark the state down when the server goes away, so the next call reconnects.
	go func() {
		_ = session.Wait()
		s.mu.Lock()
		if s.session == session {
			s.session = nil
		}
		s.mu.Unlock()
	}()

	return session, nil
}

// callTool forwards a call to the server. A transport failure drops the session and retries once on a fresh
// one; a protocol error from the server (ex: unknown tool) is returned as is.
func (s *serverState) callTool(
	ctx context.Context,
	name string,
	args json.RawMessage,
) (*mcpsdk.CallToolResult, error) {
	ctx, cancel := context.WithTimeout(ctx, s.manager.callTimeout)
	defer cancel()

	var params mcpsdk.CallToolParams
	params.Name = name
	if len(args) > 0 {
		params.Arguments = args
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		session, err := s.ensure(ctx)
		if err != nil {
			return nil, err
		}

		result, err := session.CallTool(ctx, &params)
		if err == nil {
			return result, nil
		}

		var protocolErr *jsonrpc.Error
		if errors.As(err, &protocolErr) || ctx.Err() != nil {
			return nil, err
		}

		lastErr = err
		s.drop(session)
	}

	return nil, lastErr
}

// drop forgets a session that failed, if it is still the current one.
func (s *serverState) drop(session *mcpsdk.ClientSession) {
	s.mu.Lock()
	if s.session == session {
		s.session = nil
	}
	s.mu.Unlock()

	_ = session.Close()
}

func (s *serverState) close() {
	s.mu.Lock()
	session := s.session
	s.session = nil
	s.mu.Unlock()

	if session != nil {
		_ = session.Close()
	}
}

func (s *serverState) closeIfIdle(idle time.Duration) {
	if s.cfg.Transport != TransportStdio {
		return
	}

	s.mu.Lock()
	expired := s.session != nil && time.Since(s.lastUsed) > idle
	s.mu.Unlock()

	if expired {
		s.manager.logger.Info("close idle mcp server", "server", s.cfg.Name)
		s.close()
	}
}

func dialTransport(cfg *ServerConfig) (mcpsdk.Transport, error) {
	switch cfg.Transport {
	case TransportStdio:
		cmd := exec.Command(cfg.Command, cfg.Args...)
		cmd.Env = cmd.Environ()
		for name, value := range cfg.Env {
			cmd.Env = append(cmd.Env, name+"="+value)
		}

		return &mcpsdk.CommandTransport{Command: cmd}, nil
	case TransportHTTP:
		return &mcpsdk.StreamableClientTransport{
			Endpoint:   cfg.URL,
			HTTPClient: newHeaderClient(cfg.Headers),
		}, nil
	default:
		return nil, fmt.Errorf("unknown transport %q", cfg.Transport)
	}
}
