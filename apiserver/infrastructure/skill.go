package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/taichu-lang/raven-agents/pkg/database"
	"github.com/uptrace/bun"
)

// SkillModel maps a single marketplace skill onto the `skills` table. Keep it in sync with
// migrations/postgres/04_skills.up.sql and migrations/sqlite/04_skills.up.sql.
type SkillModel struct {
	bun.BaseModel `bun:"table:skills,alias:s"`

	ID int64 `bun:"id,pk,autoincrement"`

	// Name is the name of the skill, the one declared by the skill protocol. Together with
	// Version it is unique.
	Name string `bun:"name,notnull"`

	// Provider is the author of the skill.
	Provider string `bun:"provider,notnull"`

	// Icon is a reference to the artwork of the skill, usually a URL.
	Icon string `bun:"icon,notnull"`

	// Display holds the human readable names of the skill keyed by locale, for example
	// {"en": "PDF Processing"}. It is returned as stored, clients resolve their own locale.
	Display json.RawMessage `bun:"display"`

	// Version of the skill. Free formed like the protocol one, "1.0.0" when unset.
	Version string `bun:"version,notnull"`

	// Description is the one carried by the skill manifest.
	Description string `bun:"description,notnull"`

	// SkillMd is the cached body of the SKILL.md manifest. It is not loaded by List.
	SkillMD string `bun:"skill_md,notnull"`

	// Source is the installation source of the skill, aligned with the values of
	// skill.Skill.Source, "local" for skills discovered on the local filesystem.
	Source string `bun:"source,notnull"`

	// Metadata carries the remaining attributes of the skill as JSON, like download counts.
	Metadata json.RawMessage `bun:"metadata"`

	// CreatedAt is a unix timestamp in seconds, assigned by the application layer.
	CreatedAt int64 `bun:"created_at,notnull"`
}

const (
	defaultVersion = "1.0.0"
)

var (
	// ErrSkillNotFound is returned when the targeted skill does not exist. The getters return
	// (nil, nil) instead, it serves the update and delete flows to come.
	ErrSkillNotFound = errors.New("skill: not found")

	// ErrSkillAlreadyExists is returned when a skill with the same name and version is stored.
	ErrSkillAlreadyExists = errors.New("skill: already exists")
)

type SkillStore struct {
	db     *bun.DB
	logger *slog.Logger
}

func NewSkillStore(db *bun.DB, logger *slog.Logger) *SkillStore {
	return &SkillStore{
		db:     db,
		logger: logger,
	}
}

// Create persists a new version of a skill. The name is owned by the caller, the version, the
// json columns and the creation timestamp are defaulted here when they are left empty. The
// autoincrement id of the row is written back into the model.
func (s *SkillStore) Create(ctx context.Context, skill *SkillModel) error {
	if skill == nil {
		return errors.New("skill: model is nil")
	}

	if skill.Name == "" {
		return errors.New("skill: name is required")
	}

	if skill.Version == "" {
		skill.Version = defaultVersion
	}

	if len(skill.Display) == 0 {
		skill.Display = database.EmptyJSONObject
	}

	if len(skill.Metadata) == 0 {
		skill.Metadata = database.EmptyJSONObject
	}

	if skill.CreatedAt == 0 {
		skill.CreatedAt = time.Now().Unix()
	}

	if _, err := s.db.NewInsert().
		Model(skill).
		Exec(ctx); err != nil {
		if database.IsUniqueViolation(err) {
			return fmt.Errorf("%w: %s %s", ErrSkillAlreadyExists, skill.Name, skill.Version)
		}

		return fmt.Errorf("create skill %q: %w", skill.Name, err)
	}

	s.logger.DebugContext(ctx, "created skill", "name", skill.Name, "version", skill.Version)
	return nil
}

// Get returns the skill identified by its protocol identity, the name and the version pair, or
// (nil, nil) when no such skill is stored.
func (s *SkillStore) Get(ctx context.Context, name, version string) (*SkillModel, error) {
	skill := &SkillModel{}
	if err := s.db.NewSelect().
		Model(skill).
		Where("name = ? AND version = ?", name, version).
		Scan(ctx); err != nil {
		if database.IsNoRows(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("get skill %q %q: %w", name, version, err)
	}

	return skill, nil
}

// GetByID returns the skill identified by its row id, or (nil, nil) when no such skill is stored.
func (s *SkillStore) GetByID(ctx context.Context, id int64) (*SkillModel, error) {
	skill := &SkillModel{}
	if err := s.db.NewSelect().
		Model(skill).
		Where("id = ?", id).
		Scan(ctx); err != nil {
		if database.IsNoRows(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("get skill %d: %w", id, err)
	}

	return skill, nil
}

// List returns one page of the marketplace skills, the most recently created first. A limit below
// one falls back to defaultLimit. A from above zero is a cursor: only the skills with a smaller
// id, the older ones, are returned, so from is the id of the last item of the previous page. A
// from below one means no cursor, the latest skills. The cached manifest body is not loaded:
// SkillMd is left empty, callers that need it go through Get.
func (s *SkillStore) List(ctx context.Context, from, limit int64) ([]*SkillModel, error) {
	skills := make([]*SkillModel, 0, limit)
	query := s.db.NewSelect().
		Model(&skills).
		ExcludeColumn("skill_md").
		Order("id DESC").
		Limit(limit)

	if from > 0 {
		query = query.Where("id < ?", from)
	}

	if err := query.Scan(ctx); err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}

	return skills, nil
}
