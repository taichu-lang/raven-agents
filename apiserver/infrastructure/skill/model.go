package skill

import (
	"encoding/json"

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
