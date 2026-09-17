// Package store persists CampusPulse data in DynamoDB. The same code runs against
// DynamoDB Local in Docker and against AWS.
package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/smithy-go/logging"

	"campuspulse/internal/apperr"
)

// ErrConflict means the item changed since it was read.
var ErrConflict = errors.New("item was changed by another request")

type Config struct {
	Region      string
	Endpoint    string // DynamoDB Local URL; empty for AWS
	TablePrefix string
}

type Tables struct {
	Events     string
	Rooms      string
	Alerts     string
	Requests   string
	Aggregates string
	Users      string
	Settings   string
}

type Store struct {
	db     *dynamodb.Client
	tables Tables
}

func New(ctx context.Context, cfg Config) (*Store, error) {
	opts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(cfg.Region)}
	if cfg.Endpoint != "" {
		// DynamoDB Local accepts any credentials. Fixed ones keep real AWS keys out of local runs.
		opts = append(opts, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("local", "local", "")))
		// DynamoDB Local's error responses (such as a failed duplicate check) make the
		// SDK warn "failed to close HTTP response body" on every call; it is harmless.
		opts = append(opts, awsconfig.WithLogger(logging.Nop{}))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	db := dynamodb.NewFromConfig(awsCfg, func(o *dynamodb.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
	})
	p := cfg.TablePrefix
	return &Store{db: db, tables: Tables{
		Events:     p + "events",
		Rooms:      p + "rooms",
		Alerts:     p + "alerts",
		Requests:   p + "service-requests",
		Aggregates: p + "aggregates",
		Users:      p + "users",
		Settings:   p + "settings",
	}}, nil
}

func (s *Store) Tables() Tables { return s.tables }

// Ping checks that DynamoDB is reachable and the tables exist.
func (s *Store) Ping(ctx context.Context) error {
	_, err := s.db.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(s.tables.Rooms)})
	return err
}

type index struct{ name, pk, sk string }

type tableDef struct {
	name    string
	pk, sk  string
	indexes []index
	ttl     string
}

func (s *Store) definitions() []tableDef {
	t := s.tables
	return []tableDef{
		{name: t.Events, pk: "event_id", ttl: "expires_at", indexes: []index{
			{"by_time", "day", "ts"},
			{"by_room", "room_id", "ts"},
		}},
		{name: t.Rooms, pk: "room_id"},
		{name: t.Alerts, pk: "alert_id", indexes: []index{
			{"by_status", "status", "created_key"},
			{"by_time", "kind", "created_key"},
		}},
		{name: t.Requests, pk: "request_id", indexes: []index{
			{"by_status", "status", "created_key"},
			{"by_time", "kind", "created_key"},
			{"by_creator", "creator_id", "created_key"},
		}},
		{name: t.Aggregates, pk: "pk", sk: "sk", ttl: "expires_at"},
		{name: t.Users, pk: "email"},
		{name: t.Settings, pk: "id"},
	}
}

// CreateTables creates any missing tables (on-demand capacity) and waits until they are active.
func (s *Store) CreateTables(ctx context.Context) error {
	waiter := dynamodb.NewTableExistsWaiter(s.db)
	for _, d := range s.definitions() {
		attrs := map[string]bool{}
		keySchema := func(pk, sk string) []types.KeySchemaElement {
			attrs[pk] = true
			ks := []types.KeySchemaElement{{AttributeName: aws.String(pk), KeyType: types.KeyTypeHash}}
			if sk != "" {
				attrs[sk] = true
				ks = append(ks, types.KeySchemaElement{AttributeName: aws.String(sk), KeyType: types.KeyTypeRange})
			}
			return ks
		}
		in := &dynamodb.CreateTableInput{
			TableName:   aws.String(d.name),
			KeySchema:   keySchema(d.pk, d.sk),
			BillingMode: types.BillingModePayPerRequest,
		}
		for _, ix := range d.indexes {
			in.GlobalSecondaryIndexes = append(in.GlobalSecondaryIndexes, types.GlobalSecondaryIndex{
				IndexName:  aws.String(ix.name),
				KeySchema:  keySchema(ix.pk, ix.sk),
				Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
			})
		}
		for name := range attrs {
			in.AttributeDefinitions = append(in.AttributeDefinitions, types.AttributeDefinition{
				AttributeName: aws.String(name), AttributeType: types.ScalarAttributeTypeS,
			})
		}
		_, err := s.db.CreateTable(ctx, in)
		var inUse *types.ResourceInUseException
		if err != nil && !errors.As(err, &inUse) {
			return fmt.Errorf("create table %s: %w", d.name, err)
		}
		if err := waiter.Wait(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(d.name)}, 2*time.Minute); err != nil {
			return fmt.Errorf("wait for table %s: %w", d.name, err)
		}
		if d.ttl != "" {
			// Expired events and counters are deleted automatically. DynamoDB Local
			// accepts the setting but never deletes anything, so errors are ignored.
			_, _ = s.db.UpdateTimeToLive(ctx, &dynamodb.UpdateTimeToLiveInput{
				TableName:               aws.String(d.name),
				TimeToLiveSpecification: &types.TimeToLiveSpecification{AttributeName: aws.String(d.ttl), Enabled: aws.Bool(true)},
			})
		}
	}
	return nil
}

// DeleteTables deletes all tables and their data.
func (s *Store) DeleteTables(ctx context.Context) error {
	waiter := dynamodb.NewTableNotExistsWaiter(s.db)
	for _, d := range s.definitions() {
		_, err := s.db.DeleteTable(ctx, &dynamodb.DeleteTableInput{TableName: aws.String(d.name)})
		var notFound *types.ResourceNotFoundException
		if errors.As(err, &notFound) {
			continue
		}
		if err != nil {
			return fmt.Errorf("delete table %s: %w", d.name, err)
		}
		if err := waiter.Wait(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(d.name)}, 2*time.Minute); err != nil {
			return fmt.Errorf("wait for table %s deletion: %w", d.name, err)
		}
	}
	return nil
}

func isConditionFailed(err error) bool {
	var ccf *types.ConditionalCheckFailedException
	return errors.As(err, &ccf)
}

// canceledReasons returns which items of a canceled transaction failed their condition.
func canceledReasons(err error) ([]bool, bool) {
	var tce *types.TransactionCanceledException
	if !errors.As(err, &tce) {
		return nil, false
	}
	failed := make([]bool, len(tce.CancellationReasons))
	for i, r := range tce.CancellationReasons {
		failed[i] = aws.ToString(r.Code) == "ConditionalCheckFailed"
	}
	return failed, true
}

func stringKey(name, value string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{name: &types.AttributeValueMemberS{Value: value}}
}

type item = map[string]types.AttributeValue

// Queries read at most readPageSize items per request and follow at most
// maxReadPages pages before returning a cursor, so a sparse filter cannot make
// one API call read a whole table.
const (
	readPageSize = 100
	maxReadPages = 10
)

// queryPage collects up to limit items. It returns the key to continue from, or
// nil when there are no more items. The key is the last returned item's key
// rather than DynamoDB's LastEvaluatedKey, so no item is skipped when a page
// holds more matches than needed.
func (s *Store) queryPage(ctx context.Context, in *dynamodb.QueryInput, keyAttrs []string, start item, limit int) ([]item, item, error) {
	in.Limit = aws.Int32(readPageSize)
	in.ExclusiveStartKey = start
	var items []item
	for range maxReadPages {
		out, err := s.db.Query(ctx, in)
		if err != nil {
			return nil, nil, err
		}
		for i, it := range out.Items {
			items = append(items, it)
			if len(items) == limit {
				if i < len(out.Items)-1 || out.LastEvaluatedKey != nil {
					return items, keyOf(it, keyAttrs), nil
				}
				return items, nil, nil
			}
		}
		if out.LastEvaluatedKey == nil {
			return items, nil, nil
		}
		in.ExclusiveStartKey = out.LastEvaluatedKey
	}
	return items, in.ExclusiveStartKey, nil
}

// queryAll follows every page of a query.
func (s *Store) queryAll(ctx context.Context, in *dynamodb.QueryInput) ([]item, error) {
	var items []item
	p := dynamodb.NewQueryPaginator(s.db, in)
	for p.HasMorePages() {
		out, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		items = append(items, out.Items...)
	}
	return items, nil
}

func keyOf(it item, attrs []string) item {
	key := item{}
	for _, a := range attrs {
		key[a] = it[a]
	}
	return key
}

// cursor is the opaque next_cursor value: the key to resume from, plus the day
// partition for event listings that span several days.
type cursor struct {
	Key map[string]string `json:"k,omitempty"`
	Day string            `json:"d,omitempty"`
}

func encodeCursor(c cursor) *string {
	b, _ := json.Marshal(c)
	s := base64.RawURLEncoding.EncodeToString(b)
	return &s
}

func decodeCursor(s string) (cursor, error) {
	var c cursor
	if s == "" {
		return c, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || json.Unmarshal(b, &c) != nil {
		return c, apperr.Invalid("cursor is not valid")
	}
	return c, nil
}

func keyToCursor(key item) map[string]string {
	if key == nil {
		return nil
	}
	m := map[string]string{}
	for k, v := range key {
		if sv, ok := v.(*types.AttributeValueMemberS); ok {
			m[k] = sv.Value
		}
	}
	return m
}

// cursorToKey rebuilds a start key, checking it has exactly the attributes the
// query expects, so a cursor from one listing can't be replayed against another.
func cursorToKey(m map[string]string, attrs []string) (item, error) {
	if m == nil {
		return nil, nil
	}
	if len(m) != len(attrs) {
		return nil, apperr.Invalid("cursor does not match this listing")
	}
	key := item{}
	for _, a := range attrs {
		v, ok := m[a]
		if !ok {
			return nil, apperr.Invalid("cursor does not match this listing")
		}
		key[a] = &types.AttributeValueMemberS{Value: v}
	}
	return key, nil
}

// build turns expression builders into DynamoDB expression strings.
func build(b expression.Builder) (expression.Expression, error) {
	expr, err := b.Build()
	if err != nil {
		return expr, fmt.Errorf("build expression: %w", err)
	}
	return expr, nil
}

// andAll combines optional filter conditions. It returns false when there are none.
func andAll(conds []expression.ConditionBuilder) (expression.ConditionBuilder, bool) {
	switch len(conds) {
	case 0:
		return expression.ConditionBuilder{}, false
	case 1:
		return conds[0], true
	}
	return expression.And(conds[0], conds[1], conds[2:]...), true
}

func sAttr(v string) types.AttributeValue { return &types.AttributeValueMemberS{Value: v} }
