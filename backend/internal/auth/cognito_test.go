package auth

import (
	"encoding/base64"
	"errors"
	"testing"

	"campuspulse/internal/apperr"
	"campuspulse/internal/model"
)

func TestUserFromGatewayClaims(t *testing.T) {
	base := func(groups string) map[string]string {
		return map[string]string{"sub": "abc-123", "email": "staff@northbridge.edu", "name": "Jordan Lee", "token_use": "id", "cognito:groups": groups}
	}
	tests := []struct {
		name   string
		claims map[string]string
		role   model.Role
		status int
	}{
		{"single group", base("[staff]"), model.RoleStaff, 0},
		{"most privileged group wins", base("[student admin]"), model.RoleAdmin, 0},
		{"comma separated", base("[student,staff]"), model.RoleStaff, 0},
		{"no group", base(""), "", 403},
		{"unknown group", base("[visitor]"), "", 403},
		{"access token", map[string]string{"sub": "abc-123", "token_use": "access", "cognito:groups": "[admin]"}, "", 401},
		{"no subject", map[string]string{"token_use": "id", "cognito:groups": "[admin]"}, "", 401},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := UserFromGatewayClaims(tt.claims)
			if tt.status == 0 {
				if err != nil || u.Role != tt.role || u.ID != "abc-123" || u.Name != "Jordan Lee" {
					t.Fatalf("got %+v, %v; want role %s", u, err, tt.role)
				}
				return
			}
			var ae *apperr.Error
			if !errors.As(err, &ae) || ae.Status != tt.status {
				t.Fatalf("got %+v, %v; want status %d", u, err, tt.status)
			}
		})
	}
}

func TestDecodeIDToken(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"abc-123","email":"a@b.c","name":"Alex Morgan","cognito:groups":["admin"]}`))
	c, err := decodeIDToken("header." + payload + ".signature")
	if err != nil || c.Sub != "abc-123" || c.Name != "Alex Morgan" || len(c.Groups) != 1 || c.Groups[0] != "admin" {
		t.Fatalf("decodeIDToken = %+v, %v", c, err)
	}
	if _, err := decodeIDToken("not-a-token"); err == nil {
		t.Error("expected a malformed token to be rejected")
	}
}
