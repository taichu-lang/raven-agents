package user

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/taichu-lang/raven-agents/internal/database"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
)

// ErrEmailAlreadyExists is returned when the email is already registered.
var ErrEmailAlreadyExists = errors.New("user: email already exists")

type UserStore struct {
	db     *bun.DB
	logger *slog.Logger
}

func NewUserStore(db *bun.DB, logger *slog.Logger) *UserStore {
	return &UserStore{
		db:     db,
		logger: logger,
	}
}

// CreateWithEmail persists a user together with its email credential in one transaction. The
// timestamps are owned by the caller. The autoincrement ids are written back into the models.
func (s *UserStore) CreateWithEmail(ctx context.Context, user *UserModel, email, passwordHash string) error {
	if user == nil {
		return errors.New("user: model is nil")
	}

	if len(user.Preferences) == 0 {
		user.Preferences = database.EmptyJSONObject
	}

	credential, err := json.Marshal(EmailCredential{Email: email, PasswordHash: passwordHash})
	if err != nil {
		return fmt.Errorf("marshal credential: %w", err)
	}

	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(user).Exec(ctx); err != nil {
			return fmt.Errorf("insert user: %w", err)
		}

		model := &AuthCredentialModel{
			UserID:     user.ID,
			Method:     MethodEmail,
			Credential: credential,
			CreatedAt:  user.CreatedAt,
		}
		if _, err := tx.NewInsert().Model(model).Exec(ctx); err != nil {
			return fmt.Errorf("insert credential: %w", err)
		}

		return nil
	})
	if err != nil {
		if database.IsUniqueViolation(err) {
			return ErrEmailAlreadyExists
		}

		return fmt.Errorf("create user: %w", err)
	}

	s.logger.DebugContext(ctx, "created user", "user_id", user.ID)
	return nil
}

// GetByEmail returns the user and the email credential registered with the email, or
// (nil, nil, nil) when there is none.
func (s *UserStore) GetByEmail(ctx context.Context, email string) (*UserModel, *EmailCredential, error) {
	credentialModel := &AuthCredentialModel{}
	if err := s.db.NewSelect().
		Model(credentialModel).
		Where("method = ?", MethodEmail).
		Where(s.emailExpression()+" = ?", email).
		Scan(ctx); err != nil {
		if database.IsNoRows(err) {
			return nil, nil, nil
		}

		return nil, nil, fmt.Errorf("get credential by email: %w", err)
	}

	credential := &EmailCredential{}
	if err := json.Unmarshal(credentialModel.Credential, credential); err != nil {
		return nil, nil, fmt.Errorf("unmarshal credential %d: %w", credentialModel.ID, err)
	}

	user := &UserModel{}
	if err := s.db.NewSelect().
		Model(user).
		Where("id = ?", credentialModel.UserID).
		Scan(ctx); err != nil {
		if database.IsNoRows(err) {
			return nil, nil, nil
		}

		return nil, nil, fmt.Errorf("get user %d: %w", credentialModel.UserID, err)
	}

	return user, credential, nil
}

// emailExpression extracts the email out of the credential document, the syntax is dialect
// specific. It has to match the expression of the unique index.
func (s *UserStore) emailExpression() string {
	if s.db.Dialect().Name() == dialect.PG {
		return "credential->>'email'"
	}

	return "json_extract(credential, '$.email')"
}
