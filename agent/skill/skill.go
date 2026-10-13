package skill

type Skill struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Source       string `json:"source"`
	Version      string `json:"version,omitzero"`
	Installation string `json:"installation,omitzero"`
}

// Loaded is a Skill together with the body of its manifest. It is produced by a Loader and is
// immutable: the loader hands the same value to every caller that asks for the skill.
type Loaded struct {
	Skill

	// Instructions is the body of SKILL.md, with the frontmatter stripped.
	Instructions string `json:"instructions"`
}
