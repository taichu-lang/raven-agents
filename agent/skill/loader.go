package skill

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/goccy/go-yaml"
)

const (
	// ManifestName is the file every installed skill provides at the root of its installation
	// directory. It holds a YAML frontmatter with the skill metadata, followed by the instructions
	// handed to the model.
	ManifestName = "SKILL.md"

	// DefaultMaxSize caps the size of a single manifest.
	DefaultMaxSize int64 = 1 << 20

	// SourceLocal marks a skill discovered by scanning a local directory.
	SourceLocal = "local"
)

var (
	ErrManifestNotFound = errors.New("skill manifest not found")
	ErrManifestTooLarge = errors.New("skill manifest too large")
	ErrOutsideRoot      = errors.New("skill installation escapes the scanned roots")
)

// frontmatter is the YAML header of a manifest.
type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Version     string `yaml:"version"`
}

type cacheEntry struct {
	modTime int64
	size    int64
	loaded  *Loaded
}

// Loader reads skill manifests from the local filesystem and caches them.
//
// A loader also remembers the roots it has scanned and refuses to read a manifest from outside of
// them, so that an installation path which reached the process from the outside cannot be turned
// into an arbitrary file read. A loader that has never scanned anything applies no such
// restriction, which is the case when skills are registered by hand.
type Loader struct {
	maxSize int64

	mu    sync.RWMutex
	cache map[string]*cacheEntry
	roots map[string]struct{}
}

func NewLoader(maxSize int64) *Loader {
	if maxSize <= 0 {
		maxSize = DefaultMaxSize
	}

	return &Loader{
		maxSize: maxSize,
		cache:   make(map[string]*cacheEntry),
		roots:   make(map[string]struct{}),
	}
}

// Scan returns one Skill per direct sub directory of root that holds a readable manifest, and
// allows later loads from under root. A sub directory without a manifest is skipped silently; one
// whose manifest cannot be parsed is skipped with a warning, so that a single broken skill does not
// keep the rest from being installed.
func (l *Loader) Scan(root string) ([]Skill, error) {
	cleaned := filepath.Clean(root)

	entries, err := os.ReadDir(cleaned)
	if err != nil {
		return nil, fmt.Errorf("failed to read skills directory %q: %w", cleaned, err)
	}

	l.mu.Lock()
	l.roots[cleaned] = struct{}{}
	l.mu.Unlock()

	skills := make([]Skill, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		installation := filepath.Join(cleaned, entry.Name())
		loaded, err := l.Load(Skill{Name: entry.Name(), Installation: installation})
		if err != nil {
			if !errors.Is(err, ErrManifestNotFound) {
				slog.Warn(
					"skipped a skill whose manifest could not be read",
					"skill", entry.Name(),
					"installation", installation,
					"err", err,
				)
			}
			continue
		}

		skills = append(skills, loaded.Skill)
	}

	return skills, nil
}

// Load reads the manifest of s. The result is cached and revalidated against the modification time
// of the manifest, so that editing a skill on disk takes effect without a restart while a repeated
// load costs a single stat.
//
// Fields of s act as the fallback for what the frontmatter does not declare; the returned Skill is
// what the rest of the system should use from then on.
func (l *Loader) Load(s Skill) (*Loaded, error) {
	installation, err := l.resolve(s.Installation)
	if err != nil {
		return nil, err
	}

	manifest := filepath.Join(installation, ManifestName)
	info, err := os.Stat(manifest)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrManifestNotFound, manifest)
		}
		return nil, fmt.Errorf("failed to stat %q: %w", manifest, err)
	}

	if info.Size() > l.maxSize {
		return nil, fmt.Errorf(
			"%w: %s is %d bytes, the limit is %d",
			ErrManifestTooLarge, manifest, info.Size(), l.maxSize,
		)
	}

	if cached := l.lookup(installation, info.ModTime().UnixNano(), info.Size()); cached != nil {
		return cached, nil
	}

	data, err := os.ReadFile(manifest)
	if err != nil {
		return nil, fmt.Errorf("failed to read %q: %w", manifest, err)
	}

	header, body, ok := splitFrontmatter(string(data))
	var meta frontmatter
	if ok {
		if err := yaml.Unmarshal([]byte(header), &meta); err != nil {
			return nil, fmt.Errorf("invalid frontmatter in %q: %w", manifest, err)
		}
	}

	loaded := &Loaded{
		Name:         firstNonEmpty(meta.Name, s.Name),
		Description:  firstNonEmpty(meta.Description, s.Description),
		Source:       firstNonEmpty(s.Source, SourceLocal),
		Version:      firstNonEmpty(meta.Version, s.Version),
		Installation: installation,
		Instructions: strings.TrimSpace(body),
	}

	if loaded.Name == "" {
		return nil, fmt.Errorf("skill in %q declares no name", installation)
	}

	l.store(installation, info.ModTime().UnixNano(), info.Size(), loaded)

	return loaded, nil
}

func (l *Loader) lookup(installation string, modTime, size int64) *Loaded {
	l.mu.RLock()
	defer l.mu.RUnlock()

	entry, ok := l.cache[installation]
	if !ok || entry.modTime != modTime || entry.size != size {
		return nil
	}

	return entry.loaded
}

func (l *Loader) store(installation string, modTime, size int64, loaded *Loaded) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.cache[installation] = &cacheEntry{modTime: modTime, size: size, loaded: loaded}
}

// resolve cleans an installation directory and checks that it is still contained in one of the
// scanned roots.
func (l *Loader) resolve(installation string) (string, error) {
	if installation == "" {
		return "", errors.New("skill installation directory is required")
	}

	cleaned := filepath.Clean(installation)

	l.mu.RLock()
	roots := make([]string, 0, len(l.roots))
	for root := range l.roots {
		roots = append(roots, root)
	}
	l.mu.RUnlock()

	if len(roots) == 0 {
		return cleaned, nil
	}

	for _, root := range roots {
		relative, err := filepath.Rel(root, cleaned)
		if err != nil {
			continue
		}

		if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return cleaned, nil
		}
	}

	return "", fmt.Errorf("%w: %s", ErrOutsideRoot, cleaned)
}

// splitFrontmatter separates a leading YAML frontmatter, delimited by a pair of '---' lines, from
// the rest of the manifest. A manifest without a frontmatter is returned whole as the body.
func splitFrontmatter(manifest string) (header, body string, ok bool) {
	manifest = strings.TrimPrefix(manifest, "\uFEFF")

	lines := strings.Split(manifest, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != "---" {
		return "", manifest, false
	}

	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r") != "---" {
			continue
		}

		return strings.Join(lines[1:i], "\n"), strings.Join(lines[i+1:], "\n"), true
	}

	return "", manifest, false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}
