package store

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	"campuspulse/internal/model"
)

// RoomRecord is a room's description plus its latest reading of each measurement.
// Each reading has its own timestamp, so late or out-of-order events never
// overwrite a newer value.
type RoomRecord struct {
	RoomID        string         `dynamodbav:"room_id"`
	Name          string         `dynamodbav:"name"`
	Building      string         `dynamodbav:"building"`
	Floor         int            `dynamodbav:"floor"`
	Type          model.RoomType `dynamodbav:"type"`
	Capacity      int            `dynamodbav:"capacity"`
	CreatedAt     string         `dynamodbav:"created_at"`
	Occupancy     *float64       `dynamodbav:"occupancy,omitempty"`
	OccupancyAt   string         `dynamodbav:"occupancy_at,omitempty"`
	Temperature   *float64       `dynamodbav:"temperature,omitempty"`
	TemperatureAt string         `dynamodbav:"temperature_at,omitempty"`
	Humidity      *float64       `dynamodbav:"humidity,omitempty"`
	HumidityAt    string         `dynamodbav:"humidity_at,omitempty"`
}

// UpsertRoom creates a room or updates its description, keeping its readings.
func (s *Store) UpsertRoom(ctx context.Context, r RoomRecord) error {
	update := expression.Set(expression.Name("name"), expression.Value(r.Name)).
		Set(expression.Name("building"), expression.Value(r.Building)).
		Set(expression.Name("floor"), expression.Value(r.Floor)).
		Set(expression.Name("type"), expression.Value(r.Type)).
		Set(expression.Name("capacity"), expression.Value(r.Capacity)).
		Set(expression.Name("created_at"), expression.IfNotExists(expression.Name("created_at"), expression.Value(r.CreatedAt)))
	expr, err := build(expression.NewBuilder().WithUpdate(update))
	if err != nil {
		return err
	}
	_, err = s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 aws.String(s.tables.Rooms),
		Key:                       stringKey("room_id", r.RoomID),
		UpdateExpression:          expr.Update(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	})
	return err
}

func (s *Store) ListRooms(ctx context.Context) ([]RoomRecord, error) {
	var rooms []RoomRecord
	p := dynamodb.NewScanPaginator(s.db, &dynamodb.ScanInput{TableName: aws.String(s.tables.Rooms)})
	for p.HasMorePages() {
		out, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		var page []RoomRecord
		if err := attributevalue.UnmarshalListOfMaps(out.Items, &page); err != nil {
			return nil, err
		}
		rooms = append(rooms, page...)
	}
	return rooms, nil
}

func (s *Store) GetRoom(ctx context.Context, roomID string) (*RoomRecord, error) {
	out, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{TableName: aws.String(s.tables.Rooms), Key: stringKey("room_id", roomID)})
	if err != nil || out.Item == nil {
		return nil, err
	}
	var r RoomRecord
	return &r, attributevalue.UnmarshalMap(out.Item, &r)
}

// UpdateRoomReading stores a reading ("occupancy", "temperature", or "humidity")
// if it is newer than the one the room already has.
func (s *Store) UpdateRoomReading(ctx context.Context, roomID string, metric model.EventType, value float64, at string) error {
	name, atName := expression.Name(string(metric)), expression.Name(string(metric)+"_at")
	cond := expression.AttributeExists(expression.Name("room_id")).
		And(expression.Or(expression.AttributeNotExists(atName), atName.LessThan(expression.Value(at))))
	update := expression.Set(name, expression.Value(value)).Set(atName, expression.Value(at))
	expr, err := build(expression.NewBuilder().WithCondition(cond).WithUpdate(update))
	if err != nil {
		return err
	}
	_, err = s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 aws.String(s.tables.Rooms),
		Key:                       stringKey("room_id", roomID),
		ConditionExpression:       expr.Condition(),
		UpdateExpression:          expr.Update(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	})
	if isConditionFailed(err) {
		return nil // an older reading arrived late; keep the newer one
	}
	return err
}
