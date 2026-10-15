package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/taichu-lang/raven-agents/apiserver/domain"
	"github.com/taichu-lang/raven-agents/apiserver/infrastructure/user"
	"golang.org/x/crypto/bcrypt"
)

// UserService serves the registration and the login of users.
type UserService struct {
	store *user.UserStore
}

func NewUserService(store *user.UserStore) *UserService {
	return &UserService{
		store: store,
	}
}

// Register creates a user identified by an email and a password.
func (s *UserService) Register(ctx context.Context, request *domain.RegisterRequest) (*domain.UserInfo, error) {
	email := normalizeEmail(request.Email)
	hash, err := bcrypt.GenerateFromPassword([]byte(request.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	model := &user.UserModel{
		State:          user.StateActive,
		CreatedAt:      time.Now().Unix(),
		LastAuthMethod: user.MethodEmail,
	}
	if err := s.store.CreateWithEmail(ctx, model, email, string(hash)); err != nil {
		if errors.Is(err, user.ErrEmailAlreadyExists) {
			return nil, domain.ErrEmailRegistered
		}

		return nil, fmt.Errorf("database error: %w", err)
	}

	return toUserInfo(model, email), nil
}

// Login verifies an email and a password pair.
func (s *UserService) Login(ctx context.Context, request *domain.LoginRequest) (*domain.UserInfo, error) {
	email := normalizeEmail(request.Email)
	model, credential, err := s.store.GetByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}

	if model == nil {
		return nil, domain.ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword(
		[]byte(credential.PasswordHash),
		[]byte(request.Password),
	); err != nil {
		return nil, domain.ErrInvalidCredentials
	}

	if model.State != user.StateActive {
		return nil, domain.ErrUserDisabled
	}

	return toUserInfo(model, email), nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func toUserInfo(model *user.UserModel, email string) *domain.UserInfo {
	return &domain.UserInfo{
		ID:        model.ID,
		Email:     email,
		Name:      model.Name,
		CreatedAt: model.CreatedAt,
	}
}
