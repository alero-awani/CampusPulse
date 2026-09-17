package store

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"campuspulse/internal/model"
)

const requestKind = "request"

var (
	requestStatusKey  = []string{"request_id", "status", "created_key"}
	requestTimeKey    = []string{"request_id", "kind", "created_key"}
	requestCreatorKey = []string{"request_id", "creator_id", "created_key"}
)

type RequestRecord struct {
	RequestID            string                `dynamodbav:"request_id"`
	Kind                 string                `dynamodbav:"kind"`
	Title                string                `dynamodbav:"title"`
	Description          string                `dynamodbav:"description"`
	Category             model.RequestCategory `dynamodbav:"category"`
	Building             string                `dynamodbav:"building"`
	RoomID               string                `dynamodbav:"room_id,omitempty"`
	Room                 string                `dynamodbav:"room,omitempty"`
	Priority             model.Priority        `dynamodbav:"priority"`
	Status               model.RequestStatus   `dynamodbav:"status"`
	Escalated            bool                  `dynamodbav:"escalated"`
	EscalationReason     string                `dynamodbav:"escalation_reason,omitempty"`
	EscalationSuppressed bool                  `dynamodbav:"escalation_suppressed"`
	CreatorID            string                `dynamodbav:"creator_id"`
	CreatorName          string                `dynamodbav:"creator_name"`
	AssignedTo           string                `dynamodbav:"assigned_to,omitempty"`
	CreatedAt            string                `dynamodbav:"created_at"`
	UpdatedAt            string                `dynamodbav:"updated_at"`
	DueAt                string                `dynamodbav:"due_at"`
	CreatedKey           string                `dynamodbav:"created_key"`
	History              []HistoryRecord       `dynamodbav:"history"`
}

type HistoryRecord struct {
	At     string `dynamodbav:"at"`
	By     string `dynamodbav:"by"`
	Action string `dynamodbav:"action"`
	Detail string `dynamodbav:"detail,omitempty"`
}

func (s *Store) CreateRequest(ctx context.Context, r RequestRecord) error {
	r.Kind, r.CreatedKey = requestKind, r.CreatedAt+"#"+r.RequestID
	av, err := attributevalue.MarshalMap(r)
	if err != nil {
		return err
	}
	expr, err := build(expression.NewBuilder().WithCondition(expression.AttributeNotExists(expression.Name("request_id"))))
	if err != nil {
		return err
	}
	_, err = s.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:                aws.String(s.tables.Requests),
		Item:                     av,
		ConditionExpression:      expr.Condition(),
		ExpressionAttributeNames: expr.Names(),
	})
	if isConditionFailed(err) {
		return ErrConflict
	}
	return err
}

// GetRequest returns nil if the request does not exist.
func (s *Store) GetRequest(ctx context.Context, id string) (*RequestRecord, error) {
	out, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(s.tables.Requests),
		Key:            stringKey("request_id", id),
		ConsistentRead: aws.Bool(true),
	})
	if err != nil || out.Item == nil {
		return nil, err
	}
	var r RequestRecord
	return &r, attributevalue.UnmarshalMap(out.Item, &r)
}

// RequestChanges lists the fields to change. Nil fields are left alone; an empty
// AssignedTo or EscalationReason removes the value.
type RequestChanges struct {
	Status               *model.RequestStatus
	AssignedTo           *string
	Escalated            *bool
	EscalationReason     *string
	EscalationSuppressed *bool
	History              []HistoryRecord
	UpdatedAt            string
}

// UpdateRequest applies changes if the request hasn't changed since it was read
// (optimistic locking on updated_at). It returns ErrConflict otherwise.
func (s *Store) UpdateRequest(ctx context.Context, current RequestRecord, c RequestChanges) (RequestRecord, error) {
	update := expression.Set(expression.Name("updated_at"), expression.Value(c.UpdatedAt))
	if c.Status != nil {
		update = update.Set(expression.Name("status"), expression.Value(*c.Status))
	}
	if c.AssignedTo != nil {
		update = setOrRemove(update, "assigned_to", emptyToNil(*c.AssignedTo))
	}
	if c.Escalated != nil {
		update = update.Set(expression.Name("escalated"), expression.Value(*c.Escalated))
	}
	if c.EscalationReason != nil {
		update = setOrRemove(update, "escalation_reason", emptyToNil(*c.EscalationReason))
	}
	if c.EscalationSuppressed != nil {
		update = update.Set(expression.Name("escalation_suppressed"), expression.Value(*c.EscalationSuppressed))
	}
	if len(c.History) > 0 {
		update = update.Set(expression.Name("history"), expression.ListAppend(
			expression.IfNotExists(expression.Name("history"), expression.Value([]HistoryRecord{})),
			expression.Value(c.History)))
	}
	cond := expression.Name("updated_at").Equal(expression.Value(current.UpdatedAt))
	expr, err := build(expression.NewBuilder().WithCondition(cond).WithUpdate(update))
	if err != nil {
		return RequestRecord{}, err
	}
	out, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 aws.String(s.tables.Requests),
		Key:                       stringKey("request_id", current.RequestID),
		ConditionExpression:       expr.Condition(),
		UpdateExpression:          expr.Update(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		ReturnValues:              types.ReturnValueAllNew,
	})
	if isConditionFailed(err) {
		return RequestRecord{}, ErrConflict
	}
	if err != nil {
		return RequestRecord{}, err
	}
	var updated RequestRecord
	return updated, attributevalue.UnmarshalMap(out.Attributes, &updated)
}

func emptyToNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

type RequestFilter struct {
	CreatorID string
	Statuses  []model.RequestStatus
	Priority  model.Priority
	Escalated *bool
	Building  string
	Limit     int
	Cursor    string
}

// ListRequests returns requests newest first.
func (s *Store) ListRequests(ctx context.Context, f RequestFilter) ([]RequestRecord, *string, error) {
	cur, err := decodeCursor(f.Cursor)
	if err != nil {
		return nil, nil, err
	}
	var conds []expression.ConditionBuilder
	index, keyAttrs := "by_time", requestTimeKey
	key := expression.Key("kind").Equal(expression.Value(requestKind))
	switch {
	case f.CreatorID != "":
		index, keyAttrs = "by_creator", requestCreatorKey
		key = expression.Key("creator_id").Equal(expression.Value(f.CreatorID))
	case len(f.Statuses) == 1:
		index, keyAttrs = "by_status", requestStatusKey
		key = expression.Key("status").Equal(expression.Value(f.Statuses[0]))
	}
	if index != "by_status" && len(f.Statuses) > 0 {
		values := make([]expression.OperandBuilder, len(f.Statuses))
		for i, st := range f.Statuses {
			values[i] = expression.Value(st)
		}
		conds = append(conds, expression.Name("status").In(values[0], values[1:]...))
	}
	if f.Priority != "" {
		conds = append(conds, expression.Name("priority").Equal(expression.Value(f.Priority)))
	}
	if f.Escalated != nil {
		conds = append(conds, expression.Name("escalated").Equal(expression.Value(*f.Escalated)))
	}
	if f.Building != "" {
		conds = append(conds, expression.Name("building").Equal(expression.Value(f.Building)))
	}
	b := expression.NewBuilder().WithKeyCondition(key)
	if filter, ok := andAll(conds); ok {
		b = b.WithFilter(filter)
	}
	expr, err := build(b)
	if err != nil {
		return nil, nil, err
	}
	start, err := cursorToKey(cur.Key, keyAttrs)
	if err != nil {
		return nil, nil, err
	}
	items, next, err := s.queryPage(ctx, &dynamodb.QueryInput{
		TableName:                 aws.String(s.tables.Requests),
		IndexName:                 aws.String(index),
		KeyConditionExpression:    expr.KeyCondition(),
		FilterExpression:          expr.Filter(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		ScanIndexForward:          aws.Bool(false),
	}, keyAttrs, start, f.Limit)
	if err != nil {
		return nil, nil, err
	}
	var records []RequestRecord
	if err := attributevalue.UnmarshalListOfMaps(items, &records); err != nil {
		return nil, nil, err
	}
	if next == nil {
		return records, nil, nil
	}
	return records, encodeCursor(cursor{Key: keyToCursor(next)}), nil
}

// UnresolvedRequests returns every request that is open or in progress.
func (s *Store) UnresolvedRequests(ctx context.Context) ([]RequestRecord, error) {
	var records []RequestRecord
	for _, st := range []model.RequestStatus{model.RequestOpen, model.RequestInProgress} {
		items, err := s.queryAll(ctx, &dynamodb.QueryInput{
			TableName:                 aws.String(s.tables.Requests),
			IndexName:                 aws.String("by_status"),
			KeyConditionExpression:    aws.String("#s = :s"),
			ExpressionAttributeNames:  map[string]string{"#s": "status"},
			ExpressionAttributeValues: item{":s": sAttr(string(st))},
		})
		if err != nil {
			return nil, err
		}
		var page []RequestRecord
		if err := attributevalue.UnmarshalListOfMaps(items, &page); err != nil {
			return nil, err
		}
		records = append(records, page...)
	}
	return records, nil
}
