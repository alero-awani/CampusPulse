package sim

import (
	"context"
	"os"
	"testing"
)

// TestCognitoLogin signs a student in through Cognito directly, as the simulator
// does against AWS. Set COGNITO_CLIENT_ID to run it.
func TestCognitoLogin(t *testing.T) {
	clientID := os.Getenv("COGNITO_CLIENT_ID")
	if clientID == "" {
		t.Skip("set COGNITO_CLIENT_ID to test sign-in against Cognito")
	}
	c := NewClient("https://unused.example", "")
	c.UseCognito("us-east-1", clientID)
	token, err := c.Login(context.Background(), "student@northbridge.edu", "demo1234")
	if err != nil || len(token) < 100 {
		t.Fatalf("Login = %q, %v", token, err)
	}
}
