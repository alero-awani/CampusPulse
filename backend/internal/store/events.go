package store

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	"campuspulse/internal/model"
)

// eventRetention is how long raw events are kept before DynamoDB's TTL deletes them.
// Hourly energy totals are kept separately, so dashboards keep their history.
const eventRetention = 30 * 24 * time.Hour

// eventLookbackDays limits how far back GET /events searches without a "since".
const eventLookbackDays = 7

// Events are indexed two ways: by UTC day (for recent campus-wide events) and by
// room (for a room's history). "ts" is the timestamp plus the event ID, so events
// with the same timestamp still have unique, ordered sort keys.
var (
	eventTimeKey = []string{"event_id", "day", "ts"}
	eventRoomKey = []string{"event_id", "room_id", "ts"}
)

type eventRecord struct {
	EventID    string   `dynamodbav:"event_id"`
	DeviceID   string   `dynamodbav:"device_id"`
	Building   string   `dynamodbav:"building"`
	RoomID     string   `dynamodbav:"room_id,omitempty"`
	Room       string   `dynamodbav:"room,omitempty"`
	EventType  string   `dynamodbav:"event_type"`
	ValueNum   *float64 `dynamodbav:"value_num,omitempty"`
	ValueStr   *string  `dynamodbav:"value_str,omitempty"`
	Unit       string   `dynamodbav:"unit,omitempty"`
	Severity   string   `dynamodbav:"severity"`
	Timestamp  string   `dynamodbav:"timestamp"`
	ReceivedAt string   `dynamodbav:"received_at"`
	Day        string   `dynamodbav:"day"`
	TS         string   `dynamodbav:"ts"`
	ExpiresAt  int64    `dynamodbav:"expires_at"`
}

func (r eventRecord) toModel() model.Event {
	e := model.Event{
		EventID:   r.EventID,
		DeviceID:  r.DeviceID,
		Building:  r.Building,
		EventType: model.EventType(r.EventType),
		Severity:  model.Severity(r.Severity),
		Timestamp: r.Timestamp,
	}
	if r.RoomID != "" {
		e.RoomID, e.Room = aws.String(r.RoomID), aws.String(r.Room)
	}
	if r.ValueNum != nil {
		e.Value = model.Number(*r.ValueNum)
	} else if r.ValueStr != nil {
		e.Value = model.Text(*r.ValueStr)
	}
	if r.Unit != "" {
		e.Unit = aws.String(r.Unit)
	}
	return e
}

// PutEvent stores an event unless one with the same ID exists. It returns false
// for duplicates, which makes sensor retries safe.
func (s *Store) PutEvent(ctx context.Context, e model.Event, receivedAt time.Time) (bool, error) {
	ts, err := time.Parse(model.TimeLayout, e.Timestamp)
	if err != nil {
		return false, err
	}
	rec := eventRecord{
		EventID:    e.EventID,
		DeviceID:   e.DeviceID,
		Building:   e.Building,
		RoomID:     aws.ToString(e.RoomID),
		Room:       aws.ToString(e.Room),
		EventType:  string(e.EventType),
		Unit:       aws.ToString(e.Unit),
		Severity:   string(e.Severity),
		Timestamp:  e.Timestamp,
		ReceivedAt: model.FormatTime(receivedAt),
		Day:        ts.UTC().Format(time.DateOnly),
		TS:         e.Timestamp + "#" + e.EventID,
		ExpiresAt:  ts.Add(eventRetention).Unix(),
	}
	if v, ok := e.Value.Number(); ok {
		rec.ValueNum = &v
	} else if v, ok := e.Value.Text(); ok {
		rec.ValueStr = &v
	}
	av, err := attributevalue.MarshalMap(rec)
	if err != nil {
		return false, err
	}
	expr, err := build(expression.NewBuilder().WithCondition(expression.AttributeNotExists(expression.Name("event_id"))))
	if err != nil {
		return false, err
	}
	_, err = s.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:                aws.String(s.tables.Events),
		Item:                     av,
		ConditionExpression:      expr.Condition(),
		ExpressionAttributeNames: expr.Names(),
	})
	if isConditionFailed(err) {
		return false, nil
	}
	return err == nil, err
}

type EventFilter struct {
	Building string
	RoomID   string
	Type     model.EventType
	Since    time.Time
	Limit    int
	Cursor   string
	Now      time.Time
}

// ListEvents returns events newest first.
func (s *Store) ListEvents(ctx context.Context, f EventFilter) ([]model.Event, *string, error) {
	cur, err := decodeCursor(f.Cursor)
	if err != nil {
		return nil, nil, err
	}
	var conds []expression.ConditionBuilder
	if f.Building != "" {
		conds = append(conds, expression.Name("building").Equal(expression.Value(f.Building)))
	}
	if f.Type != "" {
		conds = append(conds, expression.Name("event_type").Equal(expression.Value(string(f.Type))))
	}
	filter, hasFilter := andAll(conds)

	query := func(index string, key expression.KeyConditionBuilder) (*dynamodb.QueryInput, error) {
		if !f.Since.IsZero() {
			key = key.And(expression.Key("ts").GreaterThanEqual(expression.Value(model.FormatTime(f.Since))))
		}
		b := expression.NewBuilder().WithKeyCondition(key)
		if hasFilter {
			b = b.WithFilter(filter)
		}
		expr, err := build(b)
		if err != nil {
			return nil, err
		}
		return &dynamodb.QueryInput{
			TableName:                 aws.String(s.tables.Events),
			IndexName:                 aws.String(index),
			KeyConditionExpression:    expr.KeyCondition(),
			FilterExpression:          expr.Filter(),
			ExpressionAttributeNames:  expr.Names(),
			ExpressionAttributeValues: expr.Values(),
			ScanIndexForward:          aws.Bool(false),
		}, nil
	}

	if f.RoomID != "" {
		start, err := cursorToKey(cur.Key, eventRoomKey)
		if err != nil {
			return nil, nil, err
		}
		in, err := query("by_room", expression.Key("room_id").Equal(expression.Value(f.RoomID)))
		if err != nil {
			return nil, nil, err
		}
		items, next, err := s.queryPage(ctx, in, eventRoomKey, start, f.Limit)
		if err != nil {
			return nil, nil, err
		}
		events, err := decodeEvents(items)
		if next == nil {
			return events, nil, err
		}
		return events, encodeCursor(cursor{Key: keyToCursor(next)}), err
	}

	// Campus-wide listing: walk the day partitions from newest to oldest.
	today := f.Now.UTC().Truncate(24 * time.Hour)
	oldest := today.AddDate(0, 0, -eventLookbackDays)
	if !f.Since.IsZero() && f.Since.UTC().Truncate(24*time.Hour).After(oldest) {
		oldest = f.Since.UTC().Truncate(24 * time.Hour)
	}
	day := today
	if cur.Day != "" {
		if day, err = time.Parse(time.DateOnly, cur.Day); err != nil {
			return nil, nil, err
		}
	}
	start, err := cursorToKey(cur.Key, eventTimeKey)
	if err != nil {
		return nil, nil, err
	}
	var events []model.Event
	for ; !day.Before(oldest); day = day.AddDate(0, 0, -1) {
		in, err := query("by_time", expression.Key("day").Equal(expression.Value(day.Format(time.DateOnly))))
		if err != nil {
			return nil, nil, err
		}
		items, next, err := s.queryPage(ctx, in, eventTimeKey, start, f.Limit-len(events))
		if err != nil {
			return nil, nil, err
		}
		page, err := decodeEvents(items)
		if err != nil {
			return nil, nil, err
		}
		events = append(events, page...)
		if next != nil {
			return events, encodeCursor(cursor{Key: keyToCursor(next), Day: day.Format(time.DateOnly)}), nil
		}
		start = nil
		if len(events) == f.Limit {
			if prev := day.AddDate(0, 0, -1); !prev.Before(oldest) {
				return events, encodeCursor(cursor{Day: prev.Format(time.DateOnly)}), nil
			}
			break
		}
	}
	return events, nil, nil
}

func decodeEvents(items []item) ([]model.Event, error) {
	events := make([]model.Event, 0, len(items))
	for _, it := range items {
		var r eventRecord
		if err := attributevalue.UnmarshalMap(it, &r); err != nil {
			return nil, err
		}
		events = append(events, r.toModel())
	}
	return events, nil
}

// Reading is one room measurement used for charts.
type Reading struct {
	At    time.Time
	Type  model.EventType
	Value float64
}

// RoomReadings returns a room's occupancy, temperature, and humidity readings in [from, to], oldest first.
func (s *Store) RoomReadings(ctx context.Context, roomID string, from, to time.Time) ([]Reading, error) {
	key := expression.Key("room_id").Equal(expression.Value(roomID)).
		And(expression.Key("ts").Between(expression.Value(model.FormatTime(from)), expression.Value(model.FormatTime(to)+"~")))
	filter := expression.Name("event_type").In(
		expression.Value(string(model.EventOccupancy)),
		expression.Value(string(model.EventTemperature)),
		expression.Value(string(model.EventHumidity)),
	)
	proj := expression.NamesList(expression.Name("timestamp"), expression.Name("event_type"), expression.Name("value_num"))
	expr, err := build(expression.NewBuilder().WithKeyCondition(key).WithFilter(filter).WithProjection(proj))
	if err != nil {
		return nil, err
	}
	items, err := s.queryAll(ctx, &dynamodb.QueryInput{
		TableName:                 aws.String(s.tables.Events),
		IndexName:                 aws.String("by_room"),
		KeyConditionExpression:    expr.KeyCondition(),
		FilterExpression:          expr.Filter(),
		ProjectionExpression:      expr.Projection(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	})
	if err != nil {
		return nil, err
	}
	readings := make([]Reading, 0, len(items))
	for _, it := range items {
		var r eventRecord
		if err := attributevalue.UnmarshalMap(it, &r); err != nil {
			return nil, err
		}
		at, err := time.Parse(model.TimeLayout, r.Timestamp)
		if err != nil || r.ValueNum == nil {
			continue
		}
		readings = append(readings, Reading{At: at, Type: model.EventType(r.EventType), Value: *r.ValueNum})
	}
	return readings, nil
}

// RecentRoomEvents returns a room's latest events, newest first.
func (s *Store) RecentRoomEvents(ctx context.Context, roomID string, limit int) ([]model.Event, error) {
	events, _, err := s.ListEvents(ctx, EventFilter{RoomID: roomID, Limit: limit})
	return events, err
}
