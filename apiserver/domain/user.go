package domain

import "errors"

var (
	// ErrEmailRegistered is returned when registering an email that is already taken.
	ErrEmailRegistered = errors.New("email is already registered")

	// ErrInvalidCredentials is returned when the email or the password does not match. Both
	// cases share one error so that a login cannot be used to probe for registered emails.
	ErrInvalidCredentials = errors.New("invalid email or password")

	// ErrUserDisabled is returned when the user is not allowed to sign in.
	ErrUserDisabled = errors.New("user is disabled")
)

// UserInfo is the public view of a user.
type UserInfo struct {
	ID        int64  `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at"`
}

type RegisterRequest struct {
	Email    string `json:"email"    binding:"required,email,max=254"`
	Password string `json:"password" binding:"required,min=8,max=24"`
	Name     string `json:"name"`
}

type LoginRequest struct {
	Email    string `json:"email"    binding:"required"`
	Password string `json:"password" binding:"required"`
}
