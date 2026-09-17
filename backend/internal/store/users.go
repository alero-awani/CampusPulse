package store

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	"campuspulse/internal/model"
)

type UserRecord struct {
	Email        string     `dynamodbav:"email"`
	ID           string     `dynamodbav:"id"`
	Name         string     `dynamodbav:"name"`
	Role         model.Role `dynamodbav:"role"`
	PasswordHash string     `dynamodbav:"password_hash"`
}

func (s *Store) PutUser(ctx context.Context, u UserRecord) error {
	av, err := attributevalue.MarshalMap(u)
	if err != nil {
		return err
	}
	_, err = s.db.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(s.tables.Users), Item: av})
	return err
}

// GetUser returns nil if no account uses the email.
func (s *Store) GetUser(ctx context.Context, email string) (*UserRecord, error) {
	out, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{TableName: aws.String(s.tables.Users), Key: stringKey("email", email)})
	if err != nil || out.Item == nil {
		return nil, err
	}
	var u UserRecord
	return &u, attributevalue.UnmarshalMap(out.Item, &u)
}
