package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"github.com/awslabs/aws-lambda-go-api-proxy/core"

	"campuspulse/internal/apperr"
	"campuspulse/internal/model"
)

// Cognito signs users in with Amazon Cognito. Roles come from the user's Cognito
// groups ("student", "staff", "admin").
type Cognito struct {
	client   *cognitoidentityprovider.Client
	clientID string
	now      func() time.Time
}

func NewCognito(cfg aws.Config, clientID string) *Cognito {
	return &Cognito{client: cognitoidentityprovider.NewFromConfig(cfg), clientID: clientID, now: time.Now}
}

// Login exchanges an email and password for a Cognito ID token. Cognito checks
// the password; this code never sees a stored password or hash.
func (a *Cognito) Login(ctx context.Context, email, password string) (model.LoginResponse, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || password == "" {
		return model.LoginResponse{}, apperr.Invalid("email and password are required")
	}
	out, err := a.client.InitiateAuth(ctx, &cognitoidentityprovider.InitiateAuthInput{
		AuthFlow:       types.AuthFlowTypeUserPasswordAuth,
		ClientId:       aws.String(a.clientID),
		AuthParameters: map[string]string{"USERNAME": email, "PASSWORD": password},
	})
	if err != nil {
		var (
			notAuthorized *types.NotAuthorizedException
			notFound      *types.UserNotFoundException
			notConfirmed  *types.UserNotConfirmedException
			resetRequired *types.PasswordResetRequiredException
			tooMany       *types.TooManyRequestsException
		)
		switch {
		case errors.As(err, &tooMany):
			return model.LoginResponse{}, apperr.RateLimited()
		case errors.As(err, &notAuthorized), errors.As(err, &notFound), errors.As(err, &notConfirmed), errors.As(err, &resetRequired):
			return model.LoginResponse{}, apperr.InvalidCredentials()
		}
		return model.LoginResponse{}, err
	}
	if out.AuthenticationResult == nil || out.AuthenticationResult.IdToken == nil {
		return model.LoginResponse{}, apperr.Unauthorized("This account must finish setting up (for example, choose a new password) before it can sign in")
	}
	token := *out.AuthenticationResult.IdToken
	c, err := decodeIDToken(token)
	if err != nil {
		return model.LoginResponse{}, err
	}
	user, err := userFromClaims(c.Sub, c.Email, c.Name, c.Groups)
	if err != nil {
		return model.LoginResponse{}, err
	}
	expires := a.now().Add(time.Duration(out.AuthenticationResult.ExpiresIn) * time.Second)
	return model.LoginResponse{Token: token, ExpiresAt: model.FormatTime(expires), User: user}, nil
}

// Identify reads the user from the claims that API Gateway's JWT authorizer
// attaches after verifying the token's signature, issuer, audience, and expiry.
// It is only safe behind that authorizer; the Lambda can only be invoked by API Gateway.
func (a *Cognito) Identify(r *http.Request) (model.User, error) {
	rc, ok := core.GetAPIGatewayV2ContextFromContext(r.Context())
	if !ok || rc.Authorizer == nil || rc.Authorizer.JWT == nil {
		return model.User{}, apperr.Unauthorized("Sign in to continue")
	}
	return UserFromGatewayClaims(rc.Authorizer.JWT.Claims)
}

// UserFromGatewayClaims builds a user from API Gateway's JWT claims.
func UserFromGatewayClaims(claims map[string]string) (model.User, error) {
	if claims["token_use"] != "id" {
		return model.User{}, apperr.Unauthorized("Use the token returned by /auth/login")
	}
	// API Gateway flattens array claims into one string, such as "[staff admin]".
	groups := strings.FieldsFunc(strings.Trim(claims["cognito:groups"], "[]"), func(r rune) bool { return r == ' ' || r == ',' })
	return userFromClaims(claims["sub"], claims["email"], claims["name"], groups)
}

// userFromClaims picks the most privileged role among the user's groups.
func userFromClaims(sub, email, name string, groups []string) (model.User, error) {
	if sub == "" {
		return model.User{}, apperr.Unauthorized("Your session is invalid or has expired")
	}
	for _, role := range []model.Role{model.RoleAdmin, model.RoleStaff, model.RoleStudent} {
		if slices.Contains(groups, string(role)) {
			return model.User{ID: sub, Email: email, Name: name, Role: role}, nil
		}
	}
	return model.User{}, apperr.Forbidden() // signed in, but the account has no role
}

type idTokenClaims struct {
	Sub    string   `json:"sub"`
	Email  string   `json:"email"`
	Name   string   `json:"name"`
	Groups []string `json:"cognito:groups"`
}

// decodeIDToken reads the claims of a token that came straight from Cognito over
// HTTPS, so its signature doesn't need checking here.
func decodeIDToken(token string) (idTokenClaims, error) {
	var c idTokenClaims
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return c, errors.New("cognito returned a malformed token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(payload, &c)
}
