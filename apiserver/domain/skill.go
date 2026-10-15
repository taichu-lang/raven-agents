package domain

import "encoding/json"

// SkillItem is one entry of the skill marketplace listing, the listing view of a stored skill:
// the cached manifest body is left out because a listing does not need it. Display holds the
// human readable names keyed by locale and is returned as stored; clients resolve their own
// locale and fall back to Name when it is missing.
type SkillItem struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	Provider    string          `json:"provider"`
	Icon        string          `json:"icon"`
	Display     json.RawMessage `json:"display"`
	Version     string          `json:"version"`
	Description string          `json:"description"`
	Source      string          `json:"source"`
	Metadata    json.RawMessage `json:"metadata"`
	CreatedAt   int64           `json:"created_at"`
}

type SkillPaginationRequest struct {
	From  int64 `form:"from"`
	Limit int64 `form:"limit"`
}
