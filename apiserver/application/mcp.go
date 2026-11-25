package application

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/taichu-lang/raven-agents/agent"
	"github.com/taichu-lang/raven-agents/apiserver/infrastructure"
	"github.com/taichu-lang/raven-agents/mcp"
	"github.com/taichu-lang/raven-agents/tool"
)

// mcpConnectTimeout bounds the connection to one server while a chat request is being prepared. It is short
// on purpose: servers are connected in parallel on the request path, and a slow one is skipped.
const mcpConnectTimeout = 10 * time.Second

// MCPService resolves the MCP servers a user enabled into tools for the agent.
type MCPService struct {
	store      *infrastructure.MCPStore
	manager    *mcp.Manager
	credential cipher.AEAD
	logger     *slog.Logger
}

// NewMCPService creates the service. credentialKey is the base64 encoded 32 byte key of the config; when
// empty, servers declaring credentials are unavailable and the others are not affected.
func NewMCPService(
	store *infrastructure.MCPStore,
	manager *mcp.Manager,
	credentialKey string,
) (*MCPService, error) {
	service := &MCPService{
		store:   store,
		manager: manager,
		logger:  slog.With("service", "mcp", "provider", "mcp"),
	}

	if credentialKey == "" {
		return service, nil
	}

	key, err := base64.StdEncoding.DecodeString(credentialKey)
	if err != nil {
		return nil, fmt.Errorf("decode mcp credential key: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create mcp credential cipher: %w", err)
	}

	if service.credential, err = cipher.NewGCM(block); err != nil {
		return nil, fmt.Errorf("create mcp credential cipher: %w", err)
	}

	return service, nil
}

// SealCredentials encrypts the {NAME: value} credentials of a user for storage.
func (s *MCPService) SealCredentials(values map[string]string) (string, error) {
	if s.credential == nil {
		return "", errors.New("mcp credential key is not configured")
	}

	plain, err := json.Marshal(values)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, s.credential.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(s.credential.Seal(nonce, nonce, plain, nil)), nil
}

func (s *MCPService) openCredentials(sealed string) (map[string]string, error) {
	values := make(map[string]string)
	if sealed == "" {
		return values, nil
	}

	if s.credential == nil {
		return nil, errors.New("mcp credential key is not configured")
	}

	data, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil || len(data) < s.credential.NonceSize() {
		return nil, errors.New("malformed credentials")
	}

	nonce, body := data[:s.credential.NonceSize()], data[s.credential.NonceSize():]
	plain, err := s.credential.Open(nil, nonce, body, nil)
	if err != nil {
		return nil, fmt.Errorf("open credentials: %w", err)
	}

	if err := json.Unmarshal(plain, &values); err != nil {
		return nil, err
	}

	return values, nil
}

// resolvedServer is one enabled server of a user, ready to connect.
type resolvedServer struct {
	info   agent.MCPServerInfo
	key    string
	config mcp.ServerConfig
}

// Resolve returns the tools of the servers the user enabled, and the servers they belong to for the agent to
// list. Failures are soft: a server that is misconfigured, lacks required credentials or cannot be reached is
// skipped with a warning, so one bad server never fails the chat.
func (s *MCPService) Resolve(ctx context.Context, userID int64) ([]agent.MCPServerInfo, []tool.FuncTool) {
	servers, err := s.resolveServers(ctx, userID)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to resolve mcp servers", "user_id", userID, "err", err)
		return nil, nil
	}

	type outcome struct {
		tools []tool.FuncTool
	}

	outcomes := make([]outcome, len(servers))

	var wg sync.WaitGroup
	for i, server := range servers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tools, err := s.manager.Tools(ctx, server.key, server.config)
			if err != nil {
				s.logger.WarnContext(ctx, "skip unavailable mcp server", "server", server.info.Name, "err", err)
				return
			}

			outcomes[i].tools = tools
		}()
	}

	wg.Wait()

	infos := make([]agent.MCPServerInfo, 0, len(servers))
	tools := make([]tool.FuncTool, 0)
	for i, server := range servers {
		if len(outcomes[i].tools) == 0 {
			continue
		}

		infos = append(infos, server.info)
		tools = append(tools, outcomes[i].tools...)
	}

	return infos, tools
}

func (s *MCPService) resolveServers(ctx context.Context, userID int64) ([]resolvedServer, error) {
	states, err := s.store.ListEnabledUserServers(ctx, userID)
	if err != nil {
		return nil, err
	}

	if len(states) == 0 {
		return nil, nil
	}

	enabled := make(map[int64]*infrastructure.UserMCPServerModel, len(states))
	ids := make([]int64, 0, len(states))
	for _, state := range states {
		enabled[state.ServerID] = state
		ids = append(ids, state.ServerID)
	}

	registry, err := s.store.ListServersByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	resolved := make([]resolvedServer, 0, len(enabled))
	for _, server := range registry {
		state, ok := enabled[server.ID]
		if !ok {
			continue
		}

		config, err := s.render(server, state)
		if err != nil {
			s.logger.WarnContext(ctx, "skip mcp server", "server", server.Name, "err", err)
			continue
		}

		resolved = append(resolved, resolvedServer{
			info:   agent.MCPServerInfo{Name: server.Name, Description: server.Description},
			key:    strconv.FormatInt(userID, 10) + "/" + server.Name,
			config: *config,
		})
	}

	return resolved, nil
}

// render turns the registry entry and the credentials of the user into a connection config.
func (s *MCPService) render(
	server *infrastructure.MCPServerModel,
	state *infrastructure.UserMCPServerModel,
) (*mcp.ServerConfig, error) {
	specs, err := server.CredentialSpecs()
	if err != nil {
		return nil, err
	}

	values := make(map[string]string)
	if len(specs) > 0 {
		if values, err = s.openCredentials(state.Credentials); err != nil {
			return nil, err
		}
	}

	for _, spec := range specs {
		if spec.Required && values[spec.Name] == "" {
			return nil, fmt.Errorf("credential %q is required", spec.Name)
		}
	}

	config := &mcp.ServerConfig{
		Name:           server.Name,
		Transport:      server.Transport,
		Command:        server.Command,
		URL:            server.URL,
		ConnectTimeout: mcpConnectTimeout,
	}

	if config.Args, err = decodeStrings(server.Args); err != nil {
		return nil, fmt.Errorf("decode args: %w", err)
	}

	if config.Env, err = decodeStringMap(server.Env, values); err != nil {
		return nil, fmt.Errorf("decode env: %w", err)
	}

	if config.Headers, err = decodeStringMap(server.Headers, values); err != nil {
		return nil, fmt.Errorf("decode headers: %w", err)
	}

	return config, nil
}

func decodeStrings(raw json.RawMessage) ([]string, error) {
	var values []string
	if len(raw) == 0 {
		return values, nil
	}

	return values, json.Unmarshal(raw, &values)
}

// decodeStringMap decodes a json object and renders the ${secret.NAME} placeholders of its values.
func decodeStringMap(raw json.RawMessage, secrets map[string]string) (map[string]string, error) {
	values := make(map[string]string)
	if len(raw) == 0 {
		return values, nil
	}

	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}

	for name, value := range values {
		values[name] = infrastructure.RenderSecrets(value, secrets)
	}

	return values, nil
}
