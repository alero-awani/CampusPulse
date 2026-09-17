package service

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"campuspulse/internal/apperr"
	"campuspulse/internal/ids"
	"campuspulse/internal/model"
	"campuspulse/internal/rules"
	"campuspulse/internal/store"
)

const (
	MaxBatchSize  = 25
	ingestWorkers = 8
	maxClockSkew  = 5 * time.Minute
	maxEventAge   = 30 * 24 * time.Hour
)

var (
	idPattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)
	equipmentPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)
	doorStates       = []string{"open", "closed", "held_open", "forced_open"}
	units            = map[model.EventType]string{
		model.EventOccupancy:   "people",
		model.EventTemperature: "celsius",
		model.EventHumidity:    "percent",
		model.EventEnergy:      "kWh",
	}
	valueRanges = map[model.EventType][2]float64{
		model.EventOccupancy:   {0, 10000},
		model.EventTemperature: {-50, 100},
		model.EventHumidity:    {0, 100},
		model.EventEnergy:      {0, 100000},
	}
)

// Ingest validates, stores, and processes one event.
func (s *Service) Ingest(ctx context.Context, in model.EventInput) model.IngestResult {
	res := s.ingest(ctx, in)
	s.recordIngestion(ctx, []model.IngestResult{res})
	return res
}

// IngestBatch processes up to MaxBatchSize events in parallel. Each event gets
// its own result, so one bad event doesn't reject the others.
func (s *Service) IngestBatch(ctx context.Context, events []model.EventInput) (model.BatchResponse, error) {
	if len(events) == 0 || len(events) > MaxBatchSize {
		return model.BatchResponse{}, apperr.Invalid("events must contain between 1 and %d events", MaxBatchSize)
	}
	results := make([]model.IngestResult, len(events))
	sem := make(chan struct{}, ingestWorkers)
	var wg sync.WaitGroup
	for i, in := range events {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			results[i] = s.ingest(ctx, in)
		}()
	}
	wg.Wait()

	resp := model.BatchResponse{Results: results}
	for _, r := range results {
		switch r.Status {
		case model.IngestStored:
			resp.Accepted++
		case model.IngestDuplicate:
			resp.Duplicates++
		case model.IngestRejected:
			resp.Rejected++
		case model.IngestFailed:
			resp.Failed++
		}
	}
	s.recordIngestion(ctx, results)
	return resp, nil
}

func (s *Service) recordIngestion(ctx context.Context, results []model.IngestResult) {
	counts := map[model.IngestStatus]int{}
	for _, r := range results {
		counts[r.Status]++
	}
	if err := s.store.CountIngested(ctx, s.Now(), counts[model.IngestStored]); err != nil {
		s.log.WarnContext(ctx, "count ingested events", "error", err)
	}
	s.metrics.Count("EventsIngested", float64(counts[model.IngestStored]))
	s.metrics.Count("EventsDuplicate", float64(counts[model.IngestDuplicate]))
	s.metrics.Count("EventsRejected", float64(counts[model.IngestRejected]))
	s.metrics.Count("EventsFailed", float64(counts[model.IngestFailed]))
}

func (s *Service) ingest(ctx context.Context, in model.EventInput) model.IngestResult {
	failed := func(err error) model.IngestResult {
		s.log.ErrorContext(ctx, "ingest event", "event_id", in.EventID, "error", err)
		return model.IngestResult{EventID: in.EventID, Status: model.IngestFailed, Error: "internal error"}
	}
	layout, err := s.campusLayout(ctx)
	if err != nil {
		return failed(err)
	}
	th, err := s.currentThresholds(ctx)
	if err != nil {
		return failed(err)
	}
	now := s.Now()
	e, room, err := normalizeEvent(in, layout, now)
	if err != nil {
		return model.IngestResult{EventID: in.EventID, Status: model.IngestRejected, Error: err.Error()}
	}

	capacity := 0
	if room != nil {
		capacity = room.Capacity
	}
	var finding *rules.Finding
	e.Severity, finding = rules.EvaluateEvent(e, capacity, th.ThresholdRules)

	created, err := s.store.PutEvent(ctx, e, now)
	if err != nil {
		return failed(err)
	}
	if !created {
		return model.IngestResult{EventID: e.EventID, Status: model.IngestDuplicate}
	}
	// The event is stored; if a follow-up step fails, it is logged rather than
	// reported, because a retry would only be seen as a duplicate.
	if err := s.process(ctx, e, finding, th); err != nil {
		s.log.ErrorContext(ctx, "process event", "event_id", e.EventID, "error", err)
	}
	return model.IngestResult{EventID: e.EventID, Status: model.IngestStored, Severity: e.Severity}
}

// process updates room readings and energy totals, and raises alerts.
func (s *Service) process(ctx context.Context, e model.Event, finding *rules.Finding, th model.Thresholds) error {
	ts, err := time.Parse(model.TimeLayout, e.Timestamp)
	if err != nil {
		return err
	}
	value, _ := e.Value.Number()
	alertValue := value
	switch e.EventType {
	case model.EventOccupancy, model.EventTemperature, model.EventHumidity:
		if err := s.store.UpdateRoomReading(ctx, *e.RoomID, e.EventType, value, e.Timestamp); err != nil {
			return err
		}
	case model.EventEnergy:
		local := ts.In(s.loc)
		hourTotal, err := s.store.AddEnergy(ctx, local.Format(time.DateOnly), e.Building, local.Hour(), value)
		if err != nil {
			return err
		}
		finding = rules.EvaluateEnergyHour(e.Building, local.Hour(), hourTotal, th.Energy)
		// Energy alerts are about the hour's running total, not the single reading.
		alertValue = round(hourTotal, 2)
	}
	if finding == nil {
		return nil
	}

	rec := store.AlertRecord{
		AlertID:     ids.New("alt"),
		EventID:     e.EventID,
		Building:    e.Building,
		Type:        string(e.EventType),
		Severity:    string(finding.Severity),
		Message:     finding.Message,
		Threshold:   finding.Threshold,
		CreatedAt:   e.Timestamp,
		UpdatedAt:   model.FormatTime(s.Now()),
		LastEventAt: e.Timestamp,
		DedupKey:    finding.DedupKey,
	}
	if e.RoomID != nil {
		rec.RoomID, rec.Room = *e.RoomID, *e.Room
	}
	if e.Unit != nil {
		rec.Unit = *e.Unit
	}
	if _, ok := e.Value.Number(); ok {
		rec.ValueNum = &alertValue
	} else if v, ok := e.Value.Text(); ok {
		rec.ValueStr = &v
	}
	alert, created, err := s.store.RaiseAlert(ctx, rec)
	if err != nil {
		return err
	}
	if created {
		s.metrics.Count("AlertsRaised", 1)
		s.log.InfoContext(ctx, "alert raised", "alert_id", alert.AlertID, "severity", alert.Severity, "message", alert.Message)
	}
	return nil
}

// normalizeEvent validates a sensor event and fills in defaults: a generated ID
// when event_id is missing, the room from its name, the unit, and the time.
func normalizeEvent(in model.EventInput, c *campusIndex, now time.Time) (model.Event, *store.RoomRecord, error) {
	id := strings.TrimSpace(in.EventID)
	if id == "" {
		id = ids.New("evt")
	} else if !idPattern.MatchString(id) {
		return model.Event{}, nil, apperr.Invalid("event_id may only contain letters, digits, '.', '_', ':' and '-' (max 64 characters)")
	}
	device := strings.TrimSpace(in.DeviceID)
	if !idPattern.MatchString(device) {
		return model.Event{}, nil, apperr.Invalid("device_id is required and may only contain letters, digits, '.', '_', ':' and '-'")
	}
	if !model.Valid(in.EventType, model.EventTypes) {
		return model.Event{}, nil, apperr.Invalid("event_type must be one of: %s", model.JoinEnum(model.EventTypes))
	}
	building := strings.TrimSpace(in.Building)
	if !c.hasBuilding(building) {
		return model.Event{}, nil, apperr.Invalid("unknown building %q", building)
	}

	var room *store.RoomRecord
	switch {
	case in.RoomID != "":
		r, ok := c.rooms[in.RoomID]
		if !ok {
			return model.Event{}, nil, apperr.Invalid("unknown room_id %q", in.RoomID)
		}
		if r.Building != building {
			return model.Event{}, nil, apperr.Invalid("room %q is not in building %q", in.RoomID, building)
		}
		room = &r
	case in.Room != "":
		roomID, ok := c.byName[nameKey(building, in.Room)]
		if !ok {
			return model.Event{}, nil, apperr.Invalid("building %q has no room %q", building, in.Room)
		}
		r := c.rooms[roomID]
		room = &r
	}

	e := model.Event{EventID: id, DeviceID: device, Building: building, EventType: in.EventType, Value: in.Value}
	if room != nil {
		e.RoomID, e.Room = &room.RoomID, &room.Name
	}

	switch in.EventType {
	case model.EventOccupancy, model.EventTemperature, model.EventHumidity, model.EventEnergy:
		if room == nil && in.EventType != model.EventEnergy {
			return model.Event{}, nil, apperr.Invalid("%s events need a room_id", in.EventType)
		}
		v, ok := in.Value.Number()
		if !ok {
			return model.Event{}, nil, apperr.Invalid("value must be a number for %s events", in.EventType)
		}
		if r := valueRanges[in.EventType]; v < r[0] || v > r[1] {
			return model.Event{}, nil, apperr.Invalid("%s value must be between %g and %g", in.EventType, r[0], r[1])
		}
		if in.EventType == model.EventOccupancy && v != float64(int(v)) {
			return model.Event{}, nil, apperr.Invalid("occupancy value must be a whole number")
		}
		unit := units[in.EventType]
		if in.Unit != nil && *in.Unit != "" && !strings.EqualFold(*in.Unit, unit) {
			return model.Event{}, nil, apperr.Invalid("unit for %s events must be %q", in.EventType, unit)
		}
		e.Unit = &unit
	case model.EventDoor:
		v, ok := in.Value.Text()
		if !ok || !slices.Contains(doorStates, v) {
			return model.Event{}, nil, apperr.Invalid("door value must be one of: %s", strings.Join(doorStates, ", "))
		}
	case model.EventEquipment:
		v, ok := in.Value.Text()
		if !ok || !equipmentPattern.MatchString(v) {
			return model.Event{}, nil, apperr.Invalid(`equipment value must be a lowercase code such as "projector_failure" or "ok"`)
		}
	}
	if (in.EventType == model.EventDoor || in.EventType == model.EventEquipment) && in.Unit != nil && *in.Unit != "" {
		return model.Event{}, nil, apperr.Invalid("%s events have no unit", in.EventType)
	}

	ts := now
	if in.Timestamp != "" {
		t, err := time.Parse(time.RFC3339Nano, in.Timestamp)
		if err != nil {
			return model.Event{}, nil, apperr.Invalid("timestamp must be an ISO 8601 date-time, e.g. 2026-05-10T09:30:00Z")
		}
		if t.After(now.Add(maxClockSkew)) {
			return model.Event{}, nil, apperr.Invalid("timestamp is in the future")
		}
		if t.Before(now.Add(-maxEventAge)) {
			return model.Event{}, nil, apperr.Invalid("timestamp is more than 30 days old")
		}
		ts = t
	}
	e.Timestamp = model.FormatTime(ts)
	return e, room, nil
}
