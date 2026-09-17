// Package auth hashes passwords and issues and verifies signed session tokens.
package auth

import (
	"crypto/rand"
	"errors"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"campuspulse/internal/model"
)

const issuer = "campuspulse"

// MinPasswordLength is enforced when accounts are created.
const MinPasswordLength = 8

func HashPassword(password string) (string, error) {
	if len(password) < MinPasswordLength {
		return "", errors.New("password must be at least 8 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

var (
	dummyOnce sync.Once
	dummyHash string
)

// CheckPasswordForUnknownUser spends the same time as CheckPassword, so response
// times don't reveal which emails have accounts.
func CheckPasswordForUnknownUser(password string) {
	dummyOnce.Do(func() {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		h, _ := bcrypt.GenerateFromPassword(b, bcrypt.DefaultCost)
		dummyHash = string(h)
	})
	CheckPassword(dummyHash, password)
}

type claims struct {
	Email string     `json:"email"`
	Name  string     `json:"name"`
	Role  model.Role `json:"role"`
	jwt.RegisteredClaims
}

// Tokens issues and verifies HS256-signed JWTs.
type Tokens struct {
	secret []byte
	ttl    time.Duration
	Now    func() time.Time
}

func NewTokens(secret []byte, ttl time.Duration) (*Tokens, error) {
	if len(secret) < 32 {
		return nil, errors.New("token secret must be at least 32 bytes")
	}
	if ttl <= 0 {
		return nil, errors.New("token lifetime must be positive")
	}
	return &Tokens{secret: secret, ttl: ttl, Now: time.Now}, nil
}

func (t *Tokens) Issue(u model.User) (string, time.Time, error) {
	now := t.Now()
	expires := now.Add(t.ttl)
	c := claims{
		Email: u.Email,
		Name:  u.Name,
		Role:  u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   u.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expires),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(t.secret)
	return token, expires, err
}

// Verify checks the signature, issuer, and expiry, and returns the user in the token.
func (t *Tokens) Verify(token string) (model.User, error) {
	var c claims
	_, err := jwt.ParseWithClaims(token, &c, func(*jwt.Token) (any, error) { return t.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(issuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(t.Now),
	)
	if err != nil {
		return model.User{}, err
	}
	if c.Subject == "" || !model.Valid(c.Role, model.Roles) {
		return model.User{}, errors.New("token is missing a user or role")
	}
	return model.User{ID: c.Subject, Email: c.Email, Name: c.Name, Role: c.Role}, nil
}
