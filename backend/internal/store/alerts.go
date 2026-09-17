package store

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"campuspulse/internal/model"
)

const alertKind = "alert"

var (
	alertStatusKey = []string{"alert_id", "status", "created_key"}
	alertTimeKey   = []string{"alert_id", "kind", "created_key"}
)

type AlertRecord struct {
	AlertID     string   `dynamodbav:"alert_id"`
	Kind        string   `dynamodbav:"kind"`
	Status      string   `dynamodbav:"status"`
	CreatedKey  string   `dynamodbav:"created_key"`
	EventID     string   `dynamodbav:"event_id"`
	Building    string   `dynamodbav:"building"`
	RoomID      string   `dynamodbav:"room_id,omitempty"`
	Room        string   `dynamodbav:"room,omitempty"`
	Type        string   `dynamodbav:"type"`
	Severity    string   `dynamodbav:"severity"`
	Message     string   `dynamodbav:"message"`
	ValueNum    *float64 `dynamodbav:"value_num,omitempty"`
	ValueStr    *string  `dynamodbav:"value_str,omitempty"`
	Unit        string   `dynamodbav:"unit,omitempty"`
	Threshold   *float64 `dynamodbav:"threshold,omitempty"`
	CreatedAt   string   `dynamodbav:"created_at"`
	UpdatedAt   string   `dynamodbav:"updated_at"`
	UpdatedBy   string   `dynamodbav:"updated_by,omitempty"`
	LastEventAt string   `dynamodbav:"last_event_at"`
	DedupKey    string   `dynamodbav:"dedup_key"`
	History     []Note   `dynamodbav:"history"`
}

// Note is an audit entry for a status change made by a person.
type Note struct {
	At     string `dynamodbav:"at"`
	By     string `dynamodbav:"by"`
	Action string `dynamodbav:"action"`
	Note   string `dynamodbav:"note,omitempty"`
}

func (r AlertRecord) ToModel() model.Alert {
	a := model.Alert{
		AlertID:   r.AlertID,
		EventID:   r.EventID,
		Building:  r.Building,
		Type:      model.EventType(r.Type),
		Severity:  model.Severity(r.Severity),
		Message:   r.Message,
		Threshold: r.Threshold,
		Status:    model.AlertStatus(r.Status),
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
	if r.RoomID != "" {
		a.RoomID, a.Room = aws.String(r.RoomID), aws.String(r.Room)
	}
	if r.ValueNum != nil {
		a.Value = model.Number(*r.ValueNum)
	} else if r.ValueStr != nil {
		a.Value = model.Text(*r.ValueStr)
	}
	if r.Unit != "" {
		a.Unit = aws.String(r.Unit)
	}
	if r.UpdatedBy != "" {
		a.UpdatedBy = aws.String(r.UpdatedBy)
	}
	return a
}

// Each unresolved alert has a lock item "lock#<dedup key>" pointing at it. The
// lock stops repeated readings of the same problem from opening duplicate alerts.
// Lock items have no status or kind, so they never appear in the indexes.
func lockID(dedupKey string) string { return "lock#" + dedupKey }

// RaiseAlert opens a new alert, or updates the unresolved alert for the same
// problem. It returns true when a new alert was created.
func (s *Store) RaiseAlert(ctx context.Context, a AlertRecord) (AlertRecord, bool, error) {
	a.Kind, a.Status, a.CreatedKey = alertKind, string(model.AlertOpen), a.CreatedAt+"#"+a.AlertID
	if a.History == nil {
		a.History = []Note{}
	}
	lock := lockID(a.DedupKey)
	for range 4 {
		ref, err := s.lockRef(ctx, lock)
		if err != nil {
			return AlertRecord{}, false, err
		}
		if ref == "" {
			created, err := s.createAlert(ctx, lock, a)
			if err != nil || created {
				return a, created, err
			}
			continue // another request created it first
		}
		existing, err := s.GetAlert(ctx, ref)
		if err != nil {
			return AlertRecord{}, false, err
		}
		if existing == nil || existing.Status == string(model.AlertResolved) {
			if err := s.releaseLock(ctx, lock, ref); err != nil {
				return AlertRecord{}, false, err
			}
			continue
		}
		updated, ok, err := s.refreshAlert(ctx, *existing, a)
		if err != nil || ok {
			return updated, false, err
		}
	}
	return AlertRecord{}, false, errors.New("alert kept changing while being raised")
}

func (s *Store) lockRef(ctx context.Context, lock string) (string, error) {
	out, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(s.tables.Alerts),
		Key:            stringKey("alert_id", lock),
		ConsistentRead: aws.Bool(true),
	})
	if err != nil || out.Item == nil {
		return "", err
	}
	var rec struct {
		Ref string `dynamodbav:"alert_ref"`
	}
	err = attributevalue.UnmarshalMap(out.Item, &rec)
	return rec.Ref, err
}

func (s *Store) createAlert(ctx context.Context, lock string, a AlertRecord) (bool, error) {
	av, err := attributevalue.MarshalMap(a)
	if err != nil {
		return false, err
	}
	expr, err := build(expression.NewBuilder().WithCondition(expression.AttributeNotExists(expression.Name("alert_id"))))
	if err != nil {
		return false, err
	}
	_, err = s.db.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{TransactItems: []types.TransactWriteItem{
		{Put: &types.Put{
			TableName:                aws.String(s.tables.Alerts),
			Item:                     item{"alert_id": sAttr(lock), "alert_ref": sAttr(a.AlertID)},
			ConditionExpression:      expr.Condition(),
			ExpressionAttributeNames: expr.Names(),
		}},
		{Put: &types.Put{
			TableName:                aws.String(s.tables.Alerts),
			Item:                     av,
			ConditionExpression:      expr.Condition(),
			ExpressionAttributeNames: expr.Names(),
		}},
	}})
	if _, canceled := canceledReasons(err); canceled {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) releaseLock(ctx context.Context, lock, ref string) error {
	expr, err := build(expression.NewBuilder().WithCondition(expression.Name("alert_ref").Equal(expression.Value(ref))))
	if err != nil {
		return err
	}
	_, err = s.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName:                 aws.String(s.tables.Alerts),
		Key:                       stringKey("alert_id", lock),
		ConditionExpression:       expr.Condition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	})
	if isConditionFailed(err) {
		return nil
	}
	return err
}

// refreshAlert applies a new reading to an unresolved alert. A more severe
// reading upgrades the alert and reopens it if it was acknowledged; a less
// severe one only records that the problem is still happening.
func (s *Store) refreshAlert(ctx context.Context, existing, next AlertRecord) (AlertRecord, bool, error) {
	if next.LastEventAt < existing.LastEventAt {
		return existing, true, nil // an older reading arrived late
	}
	update := expression.Set(expression.Name("last_event_at"), expression.Value(next.LastEventAt)).
		Set(expression.Name("event_id"), expression.Value(next.EventID))
	nextRank, existingRank := model.Severity(next.Severity).Rank(), model.Severity(existing.Severity).Rank()
	if nextRank >= existingRank {
		update = update.Set(expression.Name("message"), expression.Value(next.Message)).
			Set(expression.Name("updated_at"), expression.Value(next.UpdatedAt))
		update = setOrRemove(update, "value_num", next.ValueNum)
		update = setOrRemove(update, "value_str", next.ValueStr)
		update = setOrRemove(update, "threshold", next.Threshold)
	}
	if nextRank > existingRank {
		update = update.Set(expression.Name("severity"), expression.Value(next.Severity))
		if existing.Status == string(model.AlertAcknowledged) {
			update = update.Set(expression.Name("status"), expression.Value(string(model.AlertOpen)))
		}
	}
	cond := expression.Name("status").Equal(expression.Value(existing.Status))
	expr, err := build(expression.NewBuilder().WithCondition(cond).WithUpdate(update))
	if err != nil {
		return AlertRecord{}, false, err
	}
	out, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 aws.String(s.tables.Alerts),
		Key:                       stringKey("alert_id", existing.AlertID),
		ConditionExpression:       expr.Condition(),
		UpdateExpression:          expr.Update(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		ReturnValues:              types.ReturnValueAllNew,
	})
	if isConditionFailed(err) {
		return AlertRecord{}, false, nil
	}
	if err != nil {
		return AlertRecord{}, false, err
	}
	var updated AlertRecord
	return updated, true, attributevalue.UnmarshalMap(out.Attributes, &updated)
}

func setOrRemove[T any](u expression.UpdateBuilder, name string, v *T) expression.UpdateBuilder {
	if v == nil {
		return u.Remove(expression.Name(name))
	}
	return u.Set(expression.Name(name), expression.Value(*v))
}

// GetAlert returns nil if the alert does not exist.
func (s *Store) GetAlert(ctx context.Context, id string) (*AlertRecord, error) {
	out, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(s.tables.Alerts),
		Key:            stringKey("alert_id", id),
		ConsistentRead: aws.Bool(true),
	})
	if err != nil || out.Item == nil {
		return nil, err
	}
	var r AlertRecord
	if err := attributevalue.UnmarshalMap(out.Item, &r); err != nil {
		return nil, err
	}
	if r.Kind != alertKind {
		return nil, nil // a lock item, not an alert
	}
	return &r, nil
}

// SetAlertStatus changes an alert's status if it still has the status the caller
// read. Resolving an alert also releases its lock, so the next occurrence of the
// problem opens a new alert.
func (s *Store) SetAlertStatus(ctx context.Context, current AlertRecord, to model.AlertStatus, note Note) (AlertRecord, error) {
	update := expression.Set(expression.Name("status"), expression.Value(string(to))).
		Set(expression.Name("updated_at"), expression.Value(note.At)).
		Set(expression.Name("updated_by"), expression.Value(note.By)).
		Set(expression.Name("history"), expression.ListAppend(
			expression.IfNotExists(expression.Name("history"), expression.Value([]Note{})),
			expression.Value([]Note{note})))
	cond := expression.Name("status").Equal(expression.Value(current.Status))
	expr, err := build(expression.NewBuilder().WithCondition(cond).WithUpdate(update))
	if err != nil {
		return AlertRecord{}, err
	}
	alertUpdate := &types.Update{
		TableName:                 aws.String(s.tables.Alerts),
		Key:                       stringKey("alert_id", current.AlertID),
		ConditionExpression:       expr.Condition(),
		UpdateExpression:          expr.Update(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	}

	if to == model.AlertResolved {
		lockExpr, err := build(expression.NewBuilder().WithCondition(expression.Name("alert_ref").Equal(expression.Value(current.AlertID))))
		if err != nil {
			return AlertRecord{}, err
		}
		_, err = s.db.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{TransactItems: []types.TransactWriteItem{
			{Update: alertUpdate},
			{Delete: &types.Delete{
				TableName:                 aws.String(s.tables.Alerts),
				Key:                       stringKey("alert_id", lockID(current.DedupKey)),
				ConditionExpression:       lockExpr.Condition(),
				ExpressionAttributeNames:  lockExpr.Names(),
				ExpressionAttributeValues: lockExpr.Values(),
			}},
		}})
		failed, canceled := canceledReasons(err)
		switch {
		case canceled && len(failed) > 0 && failed[0]:
			return AlertRecord{}, ErrConflict
		case canceled:
			// The lock is gone or belongs to another alert; just resolve this one.
			err = s.updateItem(ctx, alertUpdate)
		}
		if err != nil {
			return AlertRecord{}, err
		}
	} else if err := s.updateItem(ctx, alertUpdate); err != nil {
		return AlertRecord{}, err
	}

	updated, err := s.GetAlert(ctx, current.AlertID)
	if err != nil {
		return AlertRecord{}, err
	}
	if updated == nil {
		return AlertRecord{}, ErrConflict
	}
	return *updated, nil
}

func (s *Store) updateItem(ctx context.Context, u *types.Update) error {
	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 u.TableName,
		Key:                       u.Key,
		ConditionExpression:       u.ConditionExpression,
		UpdateExpression:          u.UpdateExpression,
		ExpressionAttributeNames:  u.ExpressionAttributeNames,
		ExpressionAttributeValues: u.ExpressionAttributeValues,
	})
	if isConditionFailed(err) {
		return ErrConflict
	}
	return err
}

type AlertFilter struct {
	Status   model.AlertStatus
	Severity model.Severity
	Type     model.EventType
	Building string
	Limit    int
	Cursor   string
}

// ListAlerts returns alerts newest first.
func (s *Store) ListAlerts(ctx context.Context, f AlertFilter) ([]AlertRecord, *string, error) {
	cur, err := decodeCursor(f.Cursor)
	if err != nil {
		return nil, nil, err
	}
	index, keyAttrs := "by_time", alertTimeKey
	key := expression.Key("kind").Equal(expression.Value(alertKind))
	if f.Status != "" {
		index, keyAttrs = "by_status", alertStatusKey
		key = expression.Key("status").Equal(expression.Value(string(f.Status)))
	}
	var conds []expression.ConditionBuilder
	if f.Severity != "" {
		conds = append(conds, expression.Name("severity").Equal(expression.Value(string(f.Severity))))
	}
	if f.Type != "" {
		conds = append(conds, expression.Name("type").Equal(expression.Value(string(f.Type))))
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
		TableName:                 aws.String(s.tables.Alerts),
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
	var alerts []AlertRecord
	if err := attributevalue.UnmarshalListOfMaps(items, &alerts); err != nil {
		return nil, nil, err
	}
	if next == nil {
		return alerts, nil, nil
	}
	return alerts, encodeCursor(cursor{Key: keyToCursor(next)}), nil
}

// OpenAlerts returns every alert with status "open".
func (s *Store) OpenAlerts(ctx context.Context) ([]AlertRecord, error) {
	items, err := s.queryAll(ctx, &dynamodb.QueryInput{
		TableName:                 aws.String(s.tables.Alerts),
		IndexName:                 aws.String("by_status"),
		KeyConditionExpression:    aws.String("#s = :open"),
		ExpressionAttributeNames:  map[string]string{"#s": "status"},
		ExpressionAttributeValues: item{":open": sAttr(string(model.AlertOpen))},
	})
	if err != nil {
		return nil, err
	}
	var alerts []AlertRecord
	return alerts, attributevalue.UnmarshalListOfMaps(items, &alerts)
}
