package skill

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/taichu-lang/raven-agents/internal/database"
)

// newTestStore opens a migrated in-memory sqlite database and returns a skill store on top of it.
// The in-memory database is shared state of the process, closing it drops the data, so every test
// starts from a fresh set of tables.
func newTestStore(t *testing.T) *SkillStore {
	t.Helper()

	ref, err := database.NewSqliteStore(&database.SqliteConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ref.Close() })

	return NewSkillStore(ref.DB(), ref.Logger())
}

func TestCreateAppliesDefaultsAndRejectsDuplicates(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	first := &SkillModel{
		Name:        "pdf-processing",
		Description: "Fill in PDF forms and extract text",
		SkillMD:     "# PDF processing",
	}
	if err := store.Create(ctx, first); err != nil {
		t.Fatalf("create first skill: %v", err)
	}
	if first.ID == 0 {
		t.Fatal("create did not write back the autoincrement id")
	}

	loaded, err := store.Get(ctx, "pdf-processing", "1.0.0")
	if err != nil {
		t.Fatalf("get first skill: %v", err)
	}
	if loaded == nil {
		t.Fatal("get first skill: not found")
	}
	if loaded.Version != "1.0.0" {
		t.Errorf("version = %q, want %q", loaded.Version, "1.0.0")
	}
	var display map[string]string
	if err := json.Unmarshal(loaded.Display, &display); err != nil {
		t.Fatalf("unmarshal display: %v", err)
	}
	if len(display) != 0 {
		t.Errorf("display = %v, want an empty object", display)
	}
	if loaded.SkillMD != "# PDF processing" {
		t.Errorf("skill_md = %q, want %q", loaded.SkillMD, "# PDF processing")
	}
	if loaded.CreatedAt <= 0 {
		t.Errorf("created_at = %d, want a unix timestamp", loaded.CreatedAt)
	}

	second := &SkillModel{
		Name:    "pdf-processing",
		Version: "2.0.0",
		Display: json.RawMessage(`{"en": "PDF Processing", "zh-CN": "PDF 处理"}`),
	}
	if err := store.Create(ctx, second); err != nil {
		t.Fatalf("create second version: %v", err)
	}

	duplicate := &SkillModel{Name: "pdf-processing", Version: "1.0.0"}
	if err := store.Create(ctx, duplicate); !errors.Is(err, ErrSkillAlreadyExists) {
		t.Fatalf("create duplicate = %v, want ErrSkillAlreadyExists", err)
	}

	if missing, err := store.Get(ctx, "pdf-processing", "3.0.0"); err != nil || missing != nil {
		t.Fatalf("get missing skill = (%v, %v), want (nil, nil)", missing, err)
	}
	if missing, err := store.GetByID(ctx, 404); err != nil || missing != nil {
		t.Fatalf("get skill by missing id = (%v, %v), want (nil, nil)", missing, err)
	}

	byID, err := store.GetByID(ctx, second.ID)
	if err != nil {
		t.Fatalf("get second version by id: %v", err)
	}
	if byID == nil || byID.Version != "2.0.0" {
		t.Fatalf("get second version by id = %+v, want version 2.0.0", byID)
	}

	var want, got map[string]string
	if err := json.Unmarshal(second.Display, &want); err != nil {
		t.Fatalf("unmarshal expected display: %v", err)
	}
	if err := json.Unmarshal(byID.Display, &got); err != nil {
		t.Fatalf("unmarshal stored display: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("display = %v, want %v", got, want)
	}
}

func TestListPaginatesWithoutSkillMd(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	for _, model := range []*SkillModel{
		{Name: "pdf-processing", SkillMD: "# PDF processing"},
		{Name: "web-search", SkillMD: "# Web search"},
		{Name: "excel-macros", SkillMD: "# Excel macros"},
	} {
		if err := store.Create(ctx, model); err != nil {
			t.Fatalf("create %s: %v", model.Name, err)
		}
	}

	page, err := store.List(ctx, 0, 2)
	if err != nil {
		t.Fatalf("list first page: %v", err)
	}
	if len(page) != 2 {
		t.Fatalf("first page has %d skills, want 2", len(page))
	}
	if page[0].Name != "excel-macros" || page[1].Name != "web-search" {
		t.Errorf("first page = [%s %s], want the newest skills first", page[0].Name, page[1].Name)
	}
	for _, model := range page {
		if model.SkillMD != "" {
			t.Errorf("list loaded the manifest of %s, want it left out", model.Name)
		}
	}

	page, err = store.List(ctx, page[len(page)-1].ID, 2)
	if err != nil {
		t.Fatalf("list second page: %v", err)
	}
	if len(page) != 1 || page[0].Name != "pdf-processing" {
		t.Fatalf("second page = %+v, want the single oldest skill", page)
	}

	page, err = store.List(ctx, page[len(page)-1].ID, 2)
	if err != nil {
		t.Fatalf("list past the oldest skill: %v", err)
	}
	if page == nil || len(page) != 0 {
		t.Fatalf("listing past the oldest skill = %+v, want an empty non nil page", page)
	}

	page, err = store.List(ctx, 0, 0)
	if err != nil {
		t.Fatalf("list with defaults: %v", err)
	}
	if len(page) != 3 {
		t.Fatalf("default listing has %d skills, want 3", len(page))
	}
}

func TestCreateRejectsIncompleteModel(t *testing.T) {
	store := newTestStore(t)

	if err := store.Create(context.Background(), nil); err == nil {
		t.Fatal("create nil model = nil, want an error")
	}

	if err := store.Create(context.Background(), &SkillModel{}); err == nil {
		t.Fatal("create model without name = nil, want an error")
	}
}
