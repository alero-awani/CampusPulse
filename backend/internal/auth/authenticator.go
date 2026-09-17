package auth

import (
	"context"
	"net/http"

	"campuspulse/internal/model"
)

// Authenticator signs users in and identifies who made a request. Local mode
// checks passwords and tokens itself; Cognito mode delegates both to Amazon Cognito.
type Authenticator interface {
	Login(ctx context.Context, email, password string) (model.LoginResponse, error)
	Identify(r *http.Request) (model.User, error)
}
