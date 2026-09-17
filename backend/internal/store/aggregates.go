package store

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// The aggregates table holds running totals updated as events arrive, so
// dashboards never need to scan raw events:
//
//	pk "ENERGY#<date>"        sk <building>  attributes h00..h23 (kWh per hour), total
//	pk "INGEST#<UTC hour>"    sk <minute>    attribute count (events received)

func hourAttr(h int) string { return fmt.Sprintf("h%02d", h) }

// AddEnergy adds kWh to a building's total for one hour of a campus-local date and
// returns the new total for that hour.
func (s *Store) AddEnergy(ctx context.Context, date, building string, hour int, kwh float64) (float64, error) {
	h := hourAttr(hour)
	update := expression.Add(expression.Name(h), expression.Value(kwh)).Add(expression.Name("total"), expression.Value(kwh))
	expr, err := build(expression.NewBuilder().WithUpdate(update))
	if err != nil {
		return 0, err
	}
	out, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.tables.Aggregates),
		Key: map[string]types.AttributeValue{
			"pk": sAttr("ENERGY#" + date),
			"sk": sAttr(building),
		},
		UpdateExpression:          expr.Update(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		ReturnValues:              types.ReturnValueUpdatedNew,
	})
	if err != nil {
		return 0, err
	}
	var total float64
	err = attributevalue.Unmarshal(out.Attributes[h], &total)
	return total, err
}

type EnergyRecord struct {
	Building string
	Total    float64
	Hourly   [24]float64
}

// EnergyForDate returns each building's energy totals for a campus-local date.
func (s *Store) EnergyForDate(ctx context.Context, date string) ([]EnergyRecord, error) {
	items, err := s.queryAll(ctx, &dynamodb.QueryInput{
		TableName:                 aws.String(s.tables.Aggregates),
		KeyConditionExpression:    aws.String("pk = :pk"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":pk": sAttr("ENERGY#" + date)},
	})
	if err != nil {
		return nil, err
	}
	records := make([]EnergyRecord, 0, len(items))
	for _, it := range items {
		var r EnergyRecord
		if err := attributevalue.Unmarshal(it["sk"], &r.Building); err != nil {
			return nil, err
		}
		if v, ok := it["total"]; ok {
			if err := attributevalue.Unmarshal(v, &r.Total); err != nil {
				return nil, err
			}
		}
		for h := range 24 {
			if v, ok := it[hourAttr(h)]; ok {
				if err := attributevalue.Unmarshal(v, &r.Hourly[h]); err != nil {
					return nil, err
				}
			}
		}
		records = append(records, r)
	}
	return records, nil
}

const ingestCounterRetention = 48 * time.Hour

// CountIngested adds n to the counter for the minute of at.
func (s *Store) CountIngested(ctx context.Context, at time.Time, n int) error {
	if n == 0 {
		return nil
	}
	u := at.UTC()
	update := expression.Add(expression.Name("count"), expression.Value(n)).
		Set(expression.Name("expires_at"), expression.Value(u.Add(ingestCounterRetention).Unix()))
	expr, err := build(expression.NewBuilder().WithUpdate(update))
	if err != nil {
		return err
	}
	_, err = s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.tables.Aggregates),
		Key: map[string]types.AttributeValue{
			"pk": sAttr("INGEST#" + u.Format("2006-01-02T15")),
			"sk": sAttr(u.Format("04")),
		},
		UpdateExpression:          expr.Update(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	})
	return err
}

// IngestedSince returns how many events were received in the window before now.
func (s *Store) IngestedSince(ctx context.Context, now time.Time, window time.Duration) (int, error) {
	now = now.UTC()
	from := now.Add(-window).Truncate(time.Minute)
	total := 0
	for hour := from.Truncate(time.Hour); !hour.After(now); hour = hour.Add(time.Hour) {
		items, err := s.queryAll(ctx, &dynamodb.QueryInput{
			TableName:                 aws.String(s.tables.Aggregates),
			KeyConditionExpression:    aws.String("pk = :pk"),
			ExpressionAttributeValues: map[string]types.AttributeValue{":pk": sAttr("INGEST#" + hour.Format("2006-01-02T15"))},
		})
		if err != nil {
			return 0, err
		}
		for _, it := range items {
			var rec struct {
				Minute string `dynamodbav:"sk"`
				Count  int    `dynamodbav:"count"`
			}
			if err := attributevalue.UnmarshalMap(it, &rec); err != nil {
				return 0, err
			}
			m, err := strconv.Atoi(rec.Minute)
			if err != nil {
				continue
			}
			if t := hour.Add(time.Duration(m) * time.Minute); t.After(from) && !t.After(now) {
				total += rec.Count
			}
		}
	}
	return total, nil
}
