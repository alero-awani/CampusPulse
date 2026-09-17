package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"campuspulse/internal/model"
)

var secret = []byte("test-secret-that-is-at-least-32-bytes-long")

func TestPasswords(t *testing.T) {
	if _, err := HashPassword("short"); err == nil {
		t.Error("expected short passwords to be rejected")
	}
	hash, err := HashPassword("demo1234")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(hash, "demo1234") {
		t.Error("hash contains the password")
	}
	if !CheckPassword(hash, "demo1234") || CheckPassword(hash, "demo12345") {
		t.Error("CheckPassword gave the wrong answer")
	}
}

func TestTokens(t *testing.T) {
	tokens, err := NewTokens(secret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	user := model.User{ID: "u-1", Email: "staff@northbridge.edu", Name: "Jordan Lee", Role: model.RoleStaff}
	token, _, err := tokens.Issue(user)
	if err != nil {
		t.Fatal(err)
	}
	got, err := tokens.Verify(token)
	if err != nil || got != user {
		t.Fatalf("Verify = %+v, %v; want %+v", got, err, user)
	}

	t.Run("expired", func(t *testing.T) {
		later, _ := NewTokens(secret, time.Hour)
		later.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
		if _, err := later.Verify(token); err == nil {
			t.Error("expected an expired token to be rejected")
		}
	})
	t.Run("wrong secret", func(t *testing.T) {
		other, _ := NewTokens([]byte("a-different-secret-that-is-32-bytes-long!"), time.Hour)
		if _, err := other.Verify(token); err == nil {
			t.Error("expected a token signed with another secret to be rejected")
		}
	})
	t.Run("tampered role", func(t *testing.T) {
		parts := strings.Split(token, ".")
		forged := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "u-1", "role": "admin", "iss": issuer, "exp": time.Now().Add(time.Hour).Unix()})
		forgedToken, _ := forged.SignedString([]byte("attacker-secret-attacker-secret-attacker"))
		tampered := strings.Split(forgedToken, ".")[0] + "." + strings.Split(forgedToken, ".")[1] + "." + parts[2]
		if _, err := tokens.Verify(tampered); err == nil {
			t.Error("expected a tampered token to be rejected")
		}
	})
	t.Run("alg none", func(t *testing.T) {
		unsigned := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{"sub": "u-1", "role": "admin", "iss": issuer, "exp": time.Now().Add(time.Hour).Unix()})
		s, _ := unsigned.SignedString(jwt.UnsafeAllowNoneSignatureType)
		if _, err := tokens.Verify(s); err == nil {
			t.Error("expected an unsigned token to be rejected")
		}
	})
}
