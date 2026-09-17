package store

import (
	"context"
	"encoding/json"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"campuspulse/internal/model"
)

const thresholdsID = "thresholds"

// GetThresholds returns nil if no thresholds have been saved.
func (s *Store) GetThresholds(ctx context.Context) (*model.Thresholds, error) {
	out, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(s.tables.Settings),
		Key:            stringKey("id", thresholdsID),
		ConsistentRead: aws.Bool(true),
	})
	if err != nil || out.Item == nil {
		return nil, err
	}
	v, ok := out.Item["value"].(*types.AttributeValueMemberS)
	if !ok {
		return nil, nil
	}
	var t model.Thresholds
	return &t, json.Unmarshal([]byte(v.Value), &t)
}

// PutThresholds stores the thresholds as a JSON document.
func (s *Store) PutThresholds(ctx context.Context, t model.Thresholds) error {
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	_, err = s.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.tables.Settings),
		Item:      item{"id": sAttr(thresholdsID), "value": sAttr(string(b))},
	})
	return err
}
