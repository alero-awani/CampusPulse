package auth

import (
	"context"
	"errors"
	"os"
	"testing"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"

	"campuspulse/internal/apperr"
	"campuspulse/internal/model"
)

// TestCognitoLogin signs in against a real Cognito user pool. Set COGNITO_CLIENT_ID
// (from `terraform output cognito_client_id`) to run it.
func TestCognitoLogin(t *testing.T) {
	clientID := os.Getenv("COGNITO_CLIENT_ID")
	if clientID == "" {
		t.Skip("set COGNITO_CLIENT_ID to test sign-in against Cognito")
	}
	ctx := context.Background()
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion("us-east-1"))
	if err != nil {
		t.Fatal(err)
	}
	c := NewCognito(cfg, clientID)

	resp, err := c.Login(ctx, " Admin@NorthBridge.edu ", "demo1234")
	if err != nil {
		t.Fatal(err)
	}
	if resp.User.Role != model.RoleAdmin || resp.User.Name != "Alex Morgan" || resp.User.ID == "" || resp.Token == "" {
		t.Errorf("login response = %+v", resp.User)
	}

	var ae *apperr.Error
	if _, err := c.Login(ctx, "admin@northbridge.edu", "wrong-password"); !errors.As(err, &ae) || ae.Code != "invalid_credentials" {
		t.Errorf("wrong password: %v", err)
	}
	if _, err := c.Login(ctx, "nobody@northbridge.edu", "demo1234"); !errors.As(err, &ae) || ae.Code != "invalid_credentials" {
		t.Errorf("unknown user: %v", err)
	}
}
