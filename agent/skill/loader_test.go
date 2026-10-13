package skill

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const demoManifest = `---
name: demo
description: A demo skill
version: "1.2"
---

# Demo

Do the demo thing.
`

func writeSkill(t *testing.T, root, name, manifest string) string {
	t.Helper()

	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	if manifest != "" {
		if err := os.WriteFile(filepath.Join(dir, ManifestName), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

func TestScanReadsFrontmatterAndSkipsIncompleteDirectories(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "demo", demoManifest)
	writeSkill(t, root, "no-manifest", "")
	if err := os.WriteFile(filepath.Join(root, "stray.md"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}

	skills, err := NewLoader(DefaultMaxSize).Scan(root)
	if err != nil {
		t.Fatal(err)
	}

	if len(skills) != 1 {
		t.Fatalf("expected exactly one skill, got %d: %+v", len(skills), skills)
	}

	got := skills[0]
	if got.Name != "demo" || got.Description != "A demo skill" || got.Version != "1.2" {
		t.Fatalf("unexpected metadata: %+v", got)
	}

	if got.Source != SourceLocal {
		t.Fatalf("expected source %q, got %q", SourceLocal, got.Source)
	}
}

func TestLoadStripsFrontmatter(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "demo", demoManifest)

	loader := NewLoader(DefaultMaxSize)
	if _, err := loader.Scan(root); err != nil {
		t.Fatal(err)
	}

	loaded, err := loader.Load(Skill{Name: "demo", Installation: filepath.Join(root, "demo")})
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(loaded.Instructions, "description:") {
		t.Fatalf("frontmatter leaked into the instructions: %q", loaded.Instructions)
	}

	if loaded.Instructions != "# Demo\n\nDo the demo thing." {
		t.Fatalf("unexpected instructions: %q", loaded.Instructions)
	}
}

func TestLoadRejectsAPathOutsideTheScannedRoots(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "demo", demoManifest)

	outside := t.TempDir()
	writeSkill(t, outside, "secret", demoManifest)

	loader := NewLoader(DefaultMaxSize)
	if _, err := loader.Scan(root); err != nil {
		t.Fatal(err)
	}

	// The shape an installation path smuggled in by a caller would take.
	escaping := filepath.Join(root, "demo", "..", "..", filepath.Base(outside), "secret")
	if _, err := loader.Load(Skill{Name: "secret", Installation: escaping}); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("expected ErrOutsideRoot, got %v", err)
	}
}

func TestLoadRejectsAnOversizedManifest(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "demo", demoManifest)

	loader := NewLoader(16)
	if _, err := loader.Scan(root); err != nil {
		t.Fatal(err)
	}

	_, err := loader.Load(Skill{Name: "demo", Installation: filepath.Join(root, "demo")})
	if !errors.Is(err, ErrManifestTooLarge) {
		t.Fatalf("expected ErrManifestTooLarge, got %v", err)
	}
}

func TestLoadRereadsAnEditedManifest(t *testing.T) {
	root := t.TempDir()
	dir := writeSkill(t, root, "demo", demoManifest)

	loader := NewLoader(DefaultMaxSize)
	if _, err := loader.Scan(root); err != nil {
		t.Fatal(err)
	}

	edited := strings.Replace(demoManifest, "Do the demo thing.", "Do something else.", 1)
	manifest := filepath.Join(dir, ManifestName)
	if err := os.WriteFile(manifest, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	// Size alone would not tell the two apart, so move the modification time as an edit would.
	touched := time.Now().Add(time.Second)
	if err := os.Chtimes(manifest, touched, touched); err != nil {
		t.Fatal(err)
	}

	loaded, err := loader.Load(Skill{Name: "demo", Installation: dir})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(loaded.Instructions, "Do something else.") {
		t.Fatalf("stale instructions served from the cache: %q", loaded.Instructions)
	}
}

func TestLoadReportsAMissingManifest(t *testing.T) {
	root := t.TempDir()
	dir := writeSkill(t, root, "empty", "")

	loader := NewLoader(DefaultMaxSize)
	if _, err := loader.Scan(root); err != nil {
		t.Fatal(err)
	}

	if _, err := loader.Load(Skill{Name: "empty", Installation: dir}); !errors.Is(err, ErrManifestNotFound) {
		t.Fatalf("expected ErrManifestNotFound, got %v", err)
	}
}

func TestSplitFrontmatterWithoutAHeader(t *testing.T) {
	header, body, ok := splitFrontmatter("# Just a body\n")
	if ok || header != "" || body != "# Just a body\n" {
		t.Fatalf("unexpected split: ok=%v header=%q body=%q", ok, header, body)
	}
}
