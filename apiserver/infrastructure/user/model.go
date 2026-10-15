package user

import (
	"encoding/json"

	"github.com/uptrace/bun"
)

const (
	// StateActive is the state of a user allowed to sign in.
	StateActive = "ACTIVE"

	// MethodEmail is the authentication method of the email and password pair.
	MethodEmail = "email"
)

// UserModel maps a single user onto the `users` table. Keep it in sync with
// migrations/postgres/03_users.up.sql and migrations/sqlite/03_users.up.sql.
type UserModel struct {
	bun.BaseModel `bun:"table:users,alias:u"`

	ID             int64           `bun:"id,pk,autoincrement"`
	Name           string          `bun:"name,notnull"`
	State          string          `bun:"state,notnull"`
	CreatedAt      int64           `bun:"created_at,notnull"` // unix timestamp in seconds
	LastAuthMethod string          `bun:"last_auth_method"`
	Preferences    json.RawMessage `bun:"preferences"`
}

// AuthCredentialModel maps a single credential onto the `auth_credentials` table. Credential is
// a JSON document whose shape depends on Method, see EmailCredential for the email one.
type AuthCredentialModel struct {
	bun.BaseModel `bun:"table:auth_credentials,alias:ac"`

	ID         int64           `bun:"id,pk,autoincrement"`
	UserID     int64           `bun:"user_id,notnull"`
	Method     string          `bun:"method,notnull"`
	Credential json.RawMessage `bun:"credential,notnull"`
	CreatedAt  int64           `bun:"created_at,notnull"` // unix timestamp in seconds
}

// EmailCredential is the Credential document of the MethodEmail method.
type EmailCredential struct {
	Email        string `json:"email"`
	PasswordHash string `json:"password_hash"`
}
