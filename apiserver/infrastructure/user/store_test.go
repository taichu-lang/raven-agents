package user

import (
	"context"
	"errors"
	"testing"

	"github.com/taichu-lang/raven-agents/internal/database"
)

func newTestStore(t *testing.T) *UserStore {
	t.Helper()

	ref, err := database.NewSqliteStore(&database.SqliteConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ref.Close() })

	return NewUserStore(ref.DB(), ref.Logger())
}

func TestCreateWithEmailAndGetByEmail(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	model := &UserModel{State: StateActive, CreatedAt: 100, LastAuthMethod: MethodEmail}
	if err := store.CreateWithEmail(ctx, model, "a@example.com", "hash"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if model.ID == 0 {
		t.Fatal("create did not write back the id")
	}

	loaded, credential, err := store.GetByEmail(ctx, "a@example.com")
	if err != nil {
		t.Fatalf("get by email: %v", err)
	}
	if loaded == nil || loaded.ID != model.ID || credential.PasswordHash != "hash" {
		t.Fatalf("unexpected result: %+v %+v", loaded, credential)
	}

	missing, _, err := store.GetByEmail(ctx, "b@example.com")
	if err != nil || missing != nil {
		t.Fatalf("expected (nil, nil), got %+v, %v", missing, err)
	}

	duplicate := &UserModel{State: StateActive, CreatedAt: 101}
	err = store.CreateWithEmail(ctx, duplicate, "a@example.com", "other")
	if !errors.Is(err, ErrEmailAlreadyExists) {
		t.Fatalf("expected ErrEmailAlreadyExists, got %v", err)
	}
}
