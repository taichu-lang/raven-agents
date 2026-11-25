package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/taichu-lang/raven-agents/mcp"
	"github.com/taichu-lang/raven-agents/pkg/database"
	"github.com/uptrace/bun"
)

// The transports are defined by the framework package, which is what connects with them.
const (
	TransportStdio = mcp.TransportStdio
	TransportHTTP  = mcp.TransportHTTP
)

var (
	mcpNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

	// secretPlaceholderPattern matches the ${secret.NAME} references of the env and headers values. They
	// are rendered with the credentials of the user when connecting.
	secretPlaceholderPattern = regexp.MustCompile(`\$\{secret\.([A-Za-z0-9_]+)\}`)
)

// CredentialSpec declares one credential a user has to provide before using a server. Only the declaration is
// stored on the server, the values live encrypted on the per-user row.
type CredentialSpec struct {
	Name     string `json:"name"`
	Label    string `json:"label,omitzero"`
	Required bool   `json:"required,omitzero"`
}

// MCPServerModel maps one server of the MCP registry onto the `mcp_servers` table. The registry is maintained
// by the administrator; keep the model in sync with migrations/postgres/05_mcp_servers.up.sql and
// migrations/sqlite/05_mcp_servers.up.sql.
type MCPServerModel struct {
	bun.BaseModel `bun:"table:mcp_servers,alias:ms"`

	ID int64 `bun:"id,pk,autoincrement"`

	// Name identifies the server and becomes the middle part of the tool names `mcp__<name>__<tool>`: lower
	// case letters, digits and single hyphens only, so it never contains "__".
	Name string `bun:"name,notnull"`

	// Display holds the human readable names keyed by locale, returned as stored like the skill ones.
	Display json.RawMessage `bun:"display"`

	Description string `bun:"description,notnull"`
	Icon        string `bun:"icon,notnull"`

	// Transport is TransportStdio or TransportHTTP.
	Transport string `bun:"transport,notnull"`

	// Command, Args and Env configure a stdio server. Env values may reference ${secret.NAME}.
	Command string          `bun:"command,notnull"`
	Args    json.RawMessage `bun:"args"`
	Env     json.RawMessage `bun:"env"`

	// URL and Headers configure an http server. Header values may reference ${secret.NAME}.
	URL     string          `bun:"url,notnull"`
	Headers json.RawMessage `bun:"headers"`

	// Credentials is the JSON array of CredentialSpec a user has to provide. An empty array means the server
	// needs none.
	Credentials json.RawMessage `bun:"credentials"`

	// CreatedAt and UpdatedAt are unix timestamps in seconds, assigned by the application layer.
	CreatedAt int64 `bun:"created_at,notnull"`
	UpdatedAt int64 `bun:"updated_at,notnull"`
}

// UserMCPServerModel maps the state of one user for one server onto the `mcp_user_servers` table. A missing row
// means the user never enabled the server; disabling keeps the row, so the credentials survive.
type UserMCPServerModel struct {
	bun.BaseModel `bun:"table:mcp_user_servers,alias:mus"`

	ID       int64 `bun:"id,pk,autoincrement"`
	UserID   int64 `bun:"user_id,notnull"`
	ServerID int64 `bun:"server_id,notnull"`
	Enabled  bool  `bun:"enabled,notnull"`

	// Credentials is an opaque encrypted blob of the {NAME: value} map. The store never looks into it, the
	// application layer seals and opens it with the key of the config.
	Credentials string `bun:"credentials,notnull"`

	CreatedAt int64 `bun:"created_at,notnull"`
	UpdatedAt int64 `bun:"updated_at,notnull"`
}

// CredentialSpecs decodes the declared credentials of the server.
func (m *MCPServerModel) CredentialSpecs() ([]CredentialSpec, error) {
	specs := make([]CredentialSpec, 0)
	if len(m.Credentials) == 0 {
		return specs, nil
	}

	if err := json.Unmarshal(m.Credentials, &specs); err != nil {
		return nil, fmt.Errorf("decode credentials: %w", err)
	}

	return specs, nil
}

// Validate checks the invariants the schema cannot express. Rows are written by hand, so the store validates on
// write and again on read, skipping the invalid ones instead of failing the whole listing.
func (m *MCPServerModel) Validate() error {
	if !mcpNamePattern.MatchString(m.Name) || len(m.Name) > 64 {
		return fmt.Errorf("invalid name %q: use lower case letters, digits and single hyphens", m.Name)
	}

	specs, err := m.CredentialSpecs()
	if err != nil {
		return err
	}

	declared := make(map[string]bool, len(specs))
	for _, spec := range specs {
		if spec.Name == "" {
			return errors.New("credential name is required")
		}

		declared[spec.Name] = true
	}

	var templates []string
	switch m.Transport {
	case TransportStdio:
		if m.Command == "" {
			return errors.New("command is required for the stdio transport")
		}

		templates = append(templates, string(m.Env))
	case TransportHTTP:
		if m.URL == "" {
			return errors.New("url is required for the http transport")
		}

		templates = append(templates, string(m.Headers))
	default:
		return fmt.Errorf("unknown transport %q", m.Transport)
	}

	for _, match := range secretPlaceholderPattern.FindAllStringSubmatch(strings.Join(templates, ""), -1) {
		if !declared[match[1]] {
			return fmt.Errorf("placeholder ${secret.%s} is not declared in credentials", match[1])
		}
	}

	return nil
}

// RenderSecrets replaces the ${secret.NAME} placeholders of the value with the credentials of the user. A
// placeholder without a credential renders as empty.
func RenderSecrets(value string, secrets map[string]string) string {
	return secretPlaceholderPattern.ReplaceAllStringFunc(value, func(placeholder string) string {
		return secrets[secretPlaceholderPattern.FindStringSubmatch(placeholder)[1]]
	})
}

// MCPStore persists the MCP server registry and the per-user enablement state.
type MCPStore struct {
	db     *bun.DB
	logger *slog.Logger
}

func NewMCPStore(db *bun.DB, logger *slog.Logger) *MCPStore {
	return &MCPStore{
		db:     db,
		logger: logger.With("provider", "mcp"),
	}
}

// CreateServer adds a server to the registry after validating it. The json columns and timestamps are defaulted
// here when left empty, and the autoincrement id is written back into the model.
func (s *MCPStore) CreateServer(ctx context.Context, server *MCPServerModel) error {
	if server == nil {
		return errors.New("mcp: model is nil")
	}

	if len(server.Display) == 0 {
		server.Display = database.EmptyJSONObject
	}

	if len(server.Args) == 0 {
		server.Args = database.EmptyJSONArray
	}

	if len(server.Env) == 0 {
		server.Env = database.EmptyJSONObject
	}

	if len(server.Headers) == 0 {
		server.Headers = database.EmptyJSONObject
	}

	if len(server.Credentials) == 0 {
		server.Credentials = database.EmptyJSONArray
	}

	if err := server.Validate(); err != nil {
		return fmt.Errorf("mcp: invalid server: %w", err)
	}

	now := time.Now().Unix()
	if server.CreatedAt == 0 {
		server.CreatedAt = now
	}

	if server.UpdatedAt == 0 {
		server.UpdatedAt = server.CreatedAt
	}

	if _, err := s.db.NewInsert().Model(server).Exec(ctx); err != nil {
		if database.IsUniqueViolation(err) {
			return database.ErrRecordDuplicated
		}

		return err
	}

	return nil
}

// GetServer returns the server by name, or (nil, nil) when it is not in the registry.
func (s *MCPStore) GetServer(ctx context.Context, name string) (*MCPServerModel, error) {
	server := &MCPServerModel{}
	if err := s.db.NewSelect().Model(server).Where("name = ?", name).Scan(ctx); err != nil {
		if database.IsNoRows(err) {
			return nil, nil
		}

		return nil, err
	}

	return server, nil
}

// A from above zero is a cursor: only the servers with a smaller id, the older ones, are returned,
// so from is the id of the last item of the previous page. Rows failing validation, such as hand written
// ones, are skipped with a warning, so a page may hold fewer than limit items.
func (s *MCPStore) ListServers(ctx context.Context, from, limit int64) ([]*MCPServerModel, error) {
	servers := make([]*MCPServerModel, 0, limit)
	query := s.db.NewSelect().
		Model(&servers).
		Order("id DESC").
		Limit(limit)

	if from > 0 {
		query = query.Where("id < ?", from)
	}

	if err := query.Scan(ctx); err != nil {
		return nil, err
	}

	return s.skipInvalid(ctx, servers), nil
}

// ListServersByIDs returns the servers among the given ids, ordered by name, so a caller that knows
// which servers it needs does not load the whole registry. Validation failures are skipped like ListServers.
func (s *MCPStore) ListServersByIDs(ctx context.Context, ids []int64) ([]*MCPServerModel, error) {
	servers := make([]*MCPServerModel, 0)
	if len(ids) == 0 {
		return servers, nil
	}

	if err := s.db.NewSelect().
		Model(&servers).
		Where("id IN (?)", bun.List(ids)).
		Order("name ASC").
		Scan(ctx); err != nil {
		return nil, err
	}

	return s.skipInvalid(ctx, servers), nil
}

func (s *MCPStore) skipInvalid(ctx context.Context, servers []*MCPServerModel) []*MCPServerModel {
	valid := make([]*MCPServerModel, 0, len(servers))
	for _, server := range servers {
		if err := server.Validate(); err != nil {
			s.logger.WarnContext(ctx, "skip invalid mcp server", "name", server.Name, "err", err)
			continue
		}

		valid = append(valid, server)
	}

	return valid
}

// DeleteServer removes the server and the user states referencing it in one transaction.
func (s *MCPStore) DeleteServer(ctx context.Context, name string) error {
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		server := &MCPServerModel{}
		if err := tx.NewSelect().Model(server).Where("name = ?", name).Scan(ctx); err != nil {
			if database.IsNoRows(err) {
				return database.ErrRecordNotFound
			}

			return err
		}

		if _, err := tx.NewDelete().Model((*UserMCPServerModel)(nil)).
			Where("server_id = ?", server.ID).Exec(ctx); err != nil {
			return err
		}

		if _, err := tx.NewDelete().Model(server).WherePK().Exec(ctx); err != nil {
			return err
		}

		return nil
	})
}

// ListUserServers returns the states of the user, ordered by server id.
func (s *MCPStore) ListUserServers(ctx context.Context, userID int64) ([]*UserMCPServerModel, error) {
	states := make([]*UserMCPServerModel, 0)
	if err := s.db.NewSelect().
		Model(&states).
		Where("user_id = ?", userID).
		Order("server_id ASC").
		Scan(ctx); err != nil {
		return nil, err
	}

	return states, nil
}

// ListEnabledUserServers returns the states of the user that are enabled, ordered by server id. Filtering in
// the query keeps the result small for users who disabled many servers.
func (s *MCPStore) ListEnabledUserServers(ctx context.Context, userID int64) ([]*UserMCPServerModel, error) {
	states := make([]*UserMCPServerModel, 0)
	if err := s.db.NewSelect().
		Model(&states).
		Where("user_id = ? AND enabled = ?", userID, true).
		Order("server_id ASC").
		Scan(ctx); err != nil {
		return nil, err
	}

	return states, nil
}

// GetUserServer returns the state of the user for the server, or (nil, nil) when the user never touched it.
func (s *MCPStore) GetUserServer(ctx context.Context, userID, serverID int64) (*UserMCPServerModel, error) {
	state := &UserMCPServerModel{}
	if err := s.db.NewSelect().
		Model(state).
		Where("user_id = ? AND server_id = ?", userID, serverID).
		Scan(ctx); err != nil {
		if database.IsNoRows(err) {
			return nil, nil
		}

		return nil, err
	}

	return state, nil
}

// SetEnabled enables or disables the server for the user, creating the state when missing and keeping the
// stored credentials otherwise.
func (s *MCPStore) SetEnabled(ctx context.Context, userID, serverID int64, enabled bool) error {
	now := time.Now().Unix()
	state := &UserMCPServerModel{
		UserID:    userID,
		ServerID:  serverID,
		Enabled:   enabled,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if _, err := s.db.NewInsert().
		Model(state).
		On("CONFLICT (user_id, server_id) DO UPDATE").
		Set("enabled = EXCLUDED.enabled").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx); err != nil {
		return err
	}

	return nil
}

// SetCredentials stores the sealed credentials of the user for the server. A missing state is created disabled,
// so a user can provide the credentials before enabling the server.
func (s *MCPStore) SetCredentials(ctx context.Context, userID, serverID int64, sealed string) error {
	now := time.Now().Unix()
	state := &UserMCPServerModel{
		UserID:      userID,
		ServerID:    serverID,
		Credentials: sealed,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if _, err := s.db.NewInsert().
		Model(state).
		On("CONFLICT (user_id, server_id) DO UPDATE").
		Set("credentials = EXCLUDED.credentials").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx); err != nil {
		return err
	}

	return nil
}
