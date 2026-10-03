package auth

import (
	"context"

	"campuspulse/internal/campus"
	"campuspulse/internal/model"
)

// Account is someone who can sign in in local mode.
type Account struct {
	ID           string
	Email        string
	Name         string
	Role         model.Role
	PasswordHash string
}

// Accounts finds sign-in accounts by email.
type Accounts interface {
	GetAccount(ctx context.Context, email string) (*Account, error)
}

// DemoAccounts holds the demo users in memory for local development, where there
// is no Cognito. In AWS, Cognito holds the accounts and nothing is stored here.
type DemoAccounts struct {
	byEmail map[string]Account
}

// NewDemoAccounts creates the demo users, all with the given password.
func NewDemoAccounts(password string) (*DemoAccounts, error) {
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	d := &DemoAccounts{byEmail: map[string]Account{}}
	for _, u := range campus.DemoUsers {
		d.byEmail[u.Email] = Account{ID: u.ID, Email: u.Email, Name: u.Name, Role: u.Role, PasswordHash: hash}
	}
	return d, nil
}

// GetAccount returns nil if no account uses the email.
func (d *DemoAccounts) GetAccount(_ context.Context, email string) (*Account, error) {
	a, ok := d.byEmail[email]
	if !ok {
		return nil, nil
	}
	return &a, nil
}
