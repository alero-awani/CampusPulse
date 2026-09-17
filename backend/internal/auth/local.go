package auth

import (
	"context"
	"net/http"
	"strings"

	"campuspulse/internal/apperr"
	"campuspulse/internal/model"
	"campuspulse/internal/store"
)

// UserStore finds accounts by email.
type UserStore interface {
	GetUser(ctx context.Context, email string) (*store.UserRecord, error)
}

// Local checks passwords against bcrypt hashes in the users table and issues its
// own signed tokens. It is used for local development and tests.
type Local struct {
	users  UserStore
	tokens *Tokens
}

func NewLocal(users UserStore, tokens *Tokens) *Local {
	return &Local{users: users, tokens: tokens}
}

func (a *Local) Login(ctx context.Context, email, password string) (model.LoginResponse, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || password == "" {
		return model.LoginResponse{}, apperr.Invalid("email and password are required")
	}
	u, err := a.users.GetUser(ctx, email)
	if err != nil {
		return model.LoginResponse{}, err
	}
	if u == nil {
		CheckPasswordForUnknownUser(password)
		return model.LoginResponse{}, apperr.InvalidCredentials()
	}
	if !CheckPassword(u.PasswordHash, password) {
		return model.LoginResponse{}, apperr.InvalidCredentials()
	}
	user := model.User{ID: u.ID, Email: u.Email, Name: u.Name, Role: u.Role}
	token, expires, err := a.tokens.Issue(user)
	if err != nil {
		return model.LoginResponse{}, err
	}
	return model.LoginResponse{Token: token, ExpiresAt: model.FormatTime(expires), User: user}, nil
}

func (a *Local) Identify(r *http.Request) (model.User, error) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		return model.User{}, apperr.Unauthorized("Sign in to continue")
	}
	u, err := a.tokens.Verify(token)
	if err != nil {
		return model.User{}, apperr.Unauthorized("Your session is invalid or has expired")
	}
	return u, nil
}
