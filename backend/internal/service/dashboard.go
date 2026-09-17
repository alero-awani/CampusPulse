package service

import (
	"context"
	"math"
	"sort"
	"time"

	"campuspulse/internal/apperr"
	"campuspulse/internal/model"
	"campuspulse/internal/rules"
	"campuspulse/internal/store"
)

type RoomFilter struct {
	Building string
	Status   model.RoomStatus
	Type     model.RoomType
}

// ListRooms returns every room's live state. Students don't see alert counts.
func (s *Service) ListRooms(ctx context.Context, f RoomFilter, u model.User) ([]model.Room, error) {
	if err := s.validBuilding(ctx, f.Building); err != nil {
		return nil, err
	}
	records, err := s.store.ListRooms(ctx)
	if err != nil {
		return nil, err
	}
	th, err := s.currentThresholds(ctx)
	if err != nil {
		return nil, err
	}
	openAlerts := map[string]int{}
	if isStaff(u) {
		alerts, err := s.store.OpenAlerts(ctx)
		if err != nil {
			return nil, err
		}
		for _, a := range alerts {
			if a.RoomID != "" {
				openAlerts[a.RoomID]++
			}
		}
	}
	rooms := make([]model.Room, 0, len(records))
	for _, r := range records {
		room := roomModel(r, th, openAlerts[r.RoomID])
		if (f.Building != "" && room.Building != f.Building) || (f.Status != "" && room.Status != f.Status) || (f.Type != "" && room.Type != f.Type) {
			continue
		}
		rooms = append(rooms, room)
	}
	sort.Slice(rooms, func(i, j int) bool {
		if rooms[i].Building != rooms[j].Building {
			return rooms[i].Building < rooms[j].Building
		}
		return rooms[i].Name < rooms[j].Name
	})
	return rooms, nil
}

func roomModel(r store.RoomRecord, th model.Thresholds, openAlerts int) model.Room {
	occupancy := 0
	if r.Occupancy != nil {
		occupancy = int(math.Round(*r.Occupancy))
	}
	utilization := 0.0
	if r.Capacity > 0 {
		utilization = round(float64(occupancy)/float64(r.Capacity), 3)
	}
	lastUpdated := r.CreatedAt
	for _, at := range []string{r.OccupancyAt, r.TemperatureAt, r.HumidityAt} {
		lastUpdated = max(lastUpdated, at) // fixed-width timestamps compare as strings
	}
	return model.Room{
		RoomID:      r.RoomID,
		Name:        r.Name,
		Building:    r.Building,
		Floor:       r.Floor,
		Type:        r.Type,
		Capacity:    r.Capacity,
		Occupancy:   occupancy,
		Utilization: utilization,
		Status:      rules.RoomStatus(occupancy, r.Capacity, th.Occupancy),
		Temperature: roundPtr(r.Temperature, 1),
		Humidity:    roundPtr(r.Humidity, 1),
		OpenAlerts:  openAlerts,
		LastUpdated: lastUpdated,
	}
}

const recentRoomEvents = 20

// RoomDetail returns a room with its readings over the last few hours, averaged
// into buckets for charting.
func (s *Service) RoomDetail(ctx context.Context, roomID string, hours int, u model.User) (model.RoomDetail, error) {
	if hours < 1 || hours > 24 {
		return model.RoomDetail{}, apperr.Invalid("hours must be between 1 and 24")
	}
	rec, err := s.store.GetRoom(ctx, roomID)
	if err != nil {
		return model.RoomDetail{}, err
	}
	if rec == nil {
		return model.RoomDetail{}, apperr.NotFound("room")
	}
	th, err := s.currentThresholds(ctx)
	if err != nil {
		return model.RoomDetail{}, err
	}
	openAlerts := 0
	if isStaff(u) {
		alerts, err := s.store.OpenAlerts(ctx)
		if err != nil {
			return model.RoomDetail{}, err
		}
		for _, a := range alerts {
			if a.RoomID == roomID {
				openAlerts++
			}
		}
	}
	now := s.Now()
	from := now.Add(-time.Duration(hours) * time.Hour)
	readings, err := s.store.RoomReadings(ctx, roomID, from, now)
	if err != nil {
		return model.RoomDetail{}, err
	}
	recent, err := s.store.RecentRoomEvents(ctx, roomID, recentRoomEvents)
	if err != nil {
		return model.RoomDetail{}, err
	}
	return model.RoomDetail{
		Room:         roomModel(*rec, th, openAlerts),
		Readings:     bucketReadings(readings, from, now, bucketSize(hours)),
		RecentEvents: nonNil(recent),
	}, nil
}

func bucketSize(hours int) time.Duration {
	switch {
	case hours <= 1:
		return 5 * time.Minute
	case hours <= 6:
		return 15 * time.Minute
	}
	return 30 * time.Minute
}

// bucketReadings averages readings into fixed time buckets. Buckets without a
// reading of some kind report null for it, so charts show gaps instead of zeros.
func bucketReadings(readings []store.Reading, from, to time.Time, size time.Duration) []model.RoomReading {
	start := from.Truncate(size)
	n := int(to.Sub(start)/size) + 1
	type acc struct {
		sum   [3]float64
		count [3]int
	}
	slot := map[model.EventType]int{model.EventOccupancy: 0, model.EventTemperature: 1, model.EventHumidity: 2}
	accs := make([]acc, n)
	for _, r := range readings {
		i := int(r.At.Sub(start) / size)
		k, ok := slot[r.Type]
		if i < 0 || i >= n || !ok {
			continue
		}
		accs[i].sum[k] += r.Value
		accs[i].count[k]++
	}
	avg := func(a acc, k, decimals int) *float64 {
		if a.count[k] == 0 {
			return nil
		}
		v := round(a.sum[k]/float64(a.count[k]), decimals)
		return &v
	}
	out := make([]model.RoomReading, n)
	for i, a := range accs {
		out[i] = model.RoomReading{
			Timestamp:   model.FormatTime(start.Add(time.Duration(i) * size)),
			Occupancy:   avg(a, 0, 0),
			Temperature: avg(a, 1, 1),
			Humidity:    avg(a, 2, 1),
		}
	}
	return out
}

// Stats returns the numbers for the dashboard's overview cards.
func (s *Service) Stats(ctx context.Context, building string) (model.Stats, error) {
	if err := s.validBuilding(ctx, building); err != nil {
		return model.Stats{}, err
	}
	now := s.Now()
	stats := model.Stats{GeneratedAt: model.FormatTime(now)}

	// Room counts don't need per-room alert counts, so skip that query.
	rooms, err := s.ListRooms(ctx, RoomFilter{Building: building}, model.User{Role: model.RoleStudent})
	if err != nil {
		return stats, err
	}
	for _, r := range rooms {
		stats.Rooms.Total++
		if r.Occupancy > 0 {
			stats.Rooms.Occupied++
		}
		switch r.Status {
		case model.RoomBusy:
			stats.Rooms.Busy++
		case model.RoomOverloaded:
			stats.Rooms.Overloaded++
		}
	}

	alerts, err := s.store.OpenAlerts(ctx)
	if err != nil {
		return stats, err
	}
	for _, a := range alerts {
		if building != "" && a.Building != building {
			continue
		}
		stats.Alerts.Open++
		switch model.Severity(a.Severity) {
		case model.SeverityCritical:
			stats.Alerts.Critical++
		case model.SeverityWarning:
			stats.Alerts.Warning++
		}
	}

	requests, err := s.store.UnresolvedRequests(ctx)
	if err != nil {
		return stats, err
	}
	for _, r := range requests {
		if building != "" && r.Building != building {
			continue
		}
		stats.ServiceRequests.Open++
		if r.Priority == model.PriorityUrgent {
			stats.ServiceRequests.Urgent++
		}
		if r.Escalated {
			stats.ServiceRequests.Escalated++
		}
	}

	energy, err := s.store.EnergyForDate(ctx, now.In(s.loc).Format(time.DateOnly))
	if err != nil {
		return stats, err
	}
	for _, e := range energy {
		if building != "" && e.Building != building {
			continue
		}
		stats.EnergyToday.TotalKWh += e.Total
		if top := stats.EnergyToday.TopBuilding; top == nil || e.Total > top.KWh {
			stats.EnergyToday.TopBuilding = &model.TopBuilding{Building: e.Building, KWh: e.Total}
		}
	}
	stats.EnergyToday.TotalKWh = round(stats.EnergyToday.TotalKWh, 1)
	if top := stats.EnergyToday.TopBuilding; top != nil {
		top.KWh = round(top.KWh, 1)
	}

	ingested, err := s.store.IngestedSince(ctx, now, time.Hour)
	if err != nil {
		return stats, err
	}
	stats.Ingestion = model.Ingestion{EventsLastHour: ingested, EventsPerMinute: round(float64(ingested)/60, 1)}
	return stats, nil
}

// EnergyStats returns each building's energy use for a campus-local date,
// highest first. Buildings without data are included with zeros.
func (s *Service) EnergyStats(ctx context.Context, date string) (model.EnergyStats, error) {
	if date == "" {
		date = s.Now().In(s.loc).Format(time.DateOnly)
	} else if _, err := time.Parse(time.DateOnly, date); err != nil {
		return model.EnergyStats{}, apperr.Invalid("date must look like 2026-05-10")
	}
	layout, err := s.campusLayout(ctx)
	if err != nil {
		return model.EnergyStats{}, err
	}
	records, err := s.store.EnergyForDate(ctx, date)
	if err != nil {
		return model.EnergyStats{}, err
	}
	byBuilding := map[string]store.EnergyRecord{}
	for _, b := range layout.buildings {
		byBuilding[b] = store.EnergyRecord{Building: b}
	}
	for _, r := range records {
		byBuilding[r.Building] = r
	}

	out := model.EnergyStats{Date: date, Buildings: []model.BuildingEnergy{}}
	for _, r := range byBuilding {
		out.TotalKWh += r.Total
	}
	for _, r := range byBuilding {
		b := model.BuildingEnergy{Building: r.Building, TotalKWh: round(r.Total, 1), HourlyKWh: make([]float64, 24)}
		for h, v := range r.Hourly {
			b.HourlyKWh[h] = round(v, 2)
			if v > r.Hourly[b.PeakHour] {
				b.PeakHour = h
			}
		}
		b.PeakKWh = b.HourlyKWh[b.PeakHour]
		if out.TotalKWh > 0 {
			b.Share = round(r.Total/out.TotalKWh, 3)
		}
		out.Buildings = append(out.Buildings, b)
	}
	sort.Slice(out.Buildings, func(i, j int) bool {
		bi, bj := out.Buildings[i], out.Buildings[j]
		if bi.TotalKWh != bj.TotalKWh {
			return bi.TotalKWh > bj.TotalKWh
		}
		return bi.Building < bj.Building
	})
	out.TotalKWh = round(out.TotalKWh, 1)
	return out, nil
}
