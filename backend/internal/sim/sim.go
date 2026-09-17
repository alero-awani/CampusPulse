// Package sim generates realistic campus sensor data. Rooms fill and empty
// following daily class and study patterns, temperature and humidity follow
// occupancy, and buildings use energy in proportion to the people inside.
// Problems (overcrowding, overheating, leaks, forced doors, energy spikes) are
// injected at a configurable rate and logged, so you can check that the backend
// detects them.
package sim

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"campuspulse/internal/campus"
	"campuspulse/internal/ids"
	"campuspulse/internal/model"
)

type Options struct {
	Buildings int
	// AnomalyRate is the chance, per room per tick, that a problem starts.
	AnomalyRate float64
	Seed        uint64 // 0 picks a random seed
	Location    *time.Location
	// AnomalyLog receives one JSON line per injected problem. May be nil.
	AnomalyLog io.Writer
}

type Simulator struct {
	opts      Options
	rng       *rand.Rand
	buildings []*buildingState
	started   bool
	// Anomalies counts injected problems.
	Anomalies int
}

type buildingState struct {
	building   campus.Building
	rooms      []*roomState
	spikeUntil time.Time
	spikeKW    float64
}

type roomState struct {
	room                             campus.Room
	occupancy, temperature, humidity float64
	anomaly                          string
	anomalyUntil                     time.Time
	anomalyValue                     float64
}

func New(o Options) *Simulator {
	if o.Location == nil {
		o.Location = time.UTC
	}
	seed := o.Seed
	if seed == 0 {
		seed = rand.Uint64()
	}
	s := &Simulator{opts: o, rng: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))}
	for _, b := range campus.Build(o.Buildings) {
		bs := &buildingState{building: b}
		for _, r := range b.Rooms {
			bs.rooms = append(bs.rooms, &roomState{room: r})
		}
		s.buildings = append(s.buildings, bs)
	}
	return s
}

// Rooms returns the simulated campus rooms.
func (s *Simulator) Rooms() []campus.Room {
	var rooms []campus.Room
	for _, b := range s.buildings {
		rooms = append(rooms, b.building.Rooms...)
	}
	return rooms
}

// Tick advances the simulation to now and returns the readings for the step
// that just ended.
func (s *Simulator) Tick(now time.Time, step time.Duration) []model.EventInput {
	local := now.In(s.opts.Location)
	hour := float64(local.Hour()) + float64(local.Minute())/60
	weekend := local.Weekday() == time.Saturday || local.Weekday() == time.Sunday
	ts := model.FormatTime(now)

	var events []model.EventInput
	for _, b := range s.buildings {
		people := 0.0
		for _, r := range b.rooms {
			events = append(events, s.stepRoom(now, step, hour, weekend, b, r, ts)...)
			people += r.occupancy
		}
		events = append(events, s.stepMeter(now, step, hour, weekend, b, people, ts))
	}
	s.started = true
	return events
}

func (s *Simulator) stepRoom(now time.Time, step time.Duration, hour float64, weekend bool, b *buildingState, r *roomState, ts string) []model.EventInput {
	capacity := float64(r.room.Capacity)
	target := capacity * occupancyProfile(r.room.Type, hour, weekend) * (0.85 + 0.3*s.rng.Float64())
	outdoor := math.Sin((hour - 9) / 24 * 2 * math.Pi) // warmest mid-afternoon
	if !s.started {
		r.occupancy = target
		r.temperature = 20.5 + 3*target/capacity + 0.8*outdoor
		r.humidity = 42 + 10*target/capacity
	}
	r.occupancy += (target-r.occupancy)*smoothing(step, 12*time.Minute) + s.rng.NormFloat64()*capacity*0.02
	r.occupancy = clamp(r.occupancy, 0, capacity)
	util := r.occupancy / capacity
	r.temperature += (20.5+3*util+0.8*outdoor-r.temperature)*smoothing(step, 20*time.Minute) + s.rng.NormFloat64()*0.08
	r.humidity += (42+10*util-3*outdoor-r.humidity)*smoothing(step, 30*time.Minute) + s.rng.NormFloat64()*0.4

	var events []model.EventInput
	if r.anomaly != "" && !now.Before(r.anomalyUntil) {
		r.anomaly = ""
	}
	if r.anomaly == "" && s.rng.Float64() < s.opts.AnomalyRate {
		events = append(events, s.startRoomAnomaly(now, b, r, ts)...)
	}

	occupancy, temperature, humidity := r.occupancy, r.temperature, r.humidity
	switch r.anomaly {
	case "overload":
		occupancy = capacity * r.anomalyValue
	case "heat", "cold":
		temperature = r.anomalyValue + s.rng.NormFloat64()*0.2
	case "humidity":
		humidity = r.anomalyValue + s.rng.NormFloat64()*0.5
	}

	id := r.room.ID
	events = append(events,
		s.reading("occ-"+id, r.room, model.EventOccupancy, math.Round(occupancy), ts),
		s.reading("env-"+id, r.room, model.EventTemperature, round1(temperature), ts),
		s.reading("env-"+id, r.room, model.EventHumidity, round1(clamp(humidity, 0, 100)), ts),
	)
	if s.rng.Float64() < 0.02 {
		state := "open"
		if s.rng.IntN(2) == 0 {
			state = "closed"
		}
		events = append(events, s.state("door-"+id, r.room, model.EventDoor, state, ts))
	}
	return events
}

type anomalyKind struct {
	name   string
	weight int
}

var roomAnomalies = []anomalyKind{
	{"overload", 3}, {"heat", 2}, {"cold", 1}, {"humidity", 2},
	{"door_forced", 1}, {"door_held", 1}, {"equipment", 3},
}

var (
	warningFaults  = []string{"projector_failure", "hvac_fault", "smart_board_offline", "network_down"}
	criticalFaults = []string{"water_leak", "smoke_detected"}
)

func (s *Simulator) startRoomAnomaly(now time.Time, b *buildingState, r *roomState, ts string) []model.EventInput {
	kind := s.pick(roomAnomalies)
	minutes := func(lo, hi int) time.Time {
		return now.Add(time.Duration(lo+s.rng.IntN(hi-lo+1)) * time.Minute)
	}
	var (
		events   []model.EventInput
		value    string
		expected model.Severity
	)
	switch kind {
	case "overload":
		r.anomalyValue = 1.08 + s.rng.Float64()*0.37 // 108% to 145% of capacity
		r.anomalyUntil = minutes(10, 25)
		value = fmt.Sprintf("%.0f%% of capacity", r.anomalyValue*100)
		expected = severityAbove(r.anomalyValue, 1.2)
	case "heat":
		r.anomalyValue = 28 + s.rng.Float64()*9
		r.anomalyUntil = minutes(15, 30)
		value = fmt.Sprintf("%.1f °C", r.anomalyValue)
		expected = severityAbove(r.anomalyValue, 32)
	case "cold":
		r.anomalyValue = 11 + s.rng.Float64()*6
		r.anomalyUntil = minutes(15, 30)
		value = fmt.Sprintf("%.1f °C", r.anomalyValue)
		expected = model.SeverityWarning
		if r.anomalyValue < 15 {
			expected = model.SeverityCritical
		}
	case "humidity":
		r.anomalyValue = 63 + s.rng.Float64()*25
		r.anomalyUntil = minutes(15, 30)
		value = fmt.Sprintf("%.0f%%", r.anomalyValue)
		expected = severityAbove(r.anomalyValue, 75)
	case "door_forced", "door_held":
		value, expected = "forced_open", model.SeverityCritical
		if kind == "door_held" {
			value, expected = "held_open", model.SeverityWarning
		}
		events = append(events, s.state("door-"+r.room.ID, r.room, model.EventDoor, value, ts))
		kind, r.anomalyUntil = "door", now
	case "equipment":
		value, expected = warningFaults[s.rng.IntN(len(warningFaults))], model.SeverityWarning
		if s.rng.IntN(4) == 0 {
			value, expected = criticalFaults[s.rng.IntN(len(criticalFaults))], model.SeverityCritical
		}
		events = append(events, s.state("eq-"+r.room.ID, r.room, model.EventEquipment, value, ts))
		r.anomalyUntil = now
	}
	r.anomaly = kind
	s.logAnomaly(now, kind, b.building.Name, r.room.ID, value, r.anomalyUntil, expected)
	return events
}

func (s *Simulator) stepMeter(now time.Time, step time.Duration, hour float64, weekend bool, b *buildingState, people float64, ts string) model.EventInput {
	daytime := plateau(hour, 7, 20, 1.5)
	if weekend {
		daytime *= 0.35
	}
	load := b.building.BaseLoadKW*(0.45+0.55*daytime) + 0.12*people + s.rng.NormFloat64()*b.building.BaseLoadKW*0.03

	if !now.Before(b.spikeUntil) && s.rng.Float64() < s.opts.AnomalyRate*0.5 {
		b.spikeKW = 250 + s.rng.Float64()*120
		b.spikeUntil = now.Add(time.Duration(30+s.rng.IntN(21)) * time.Minute)
		s.logAnomaly(now, "energy_spike", b.building.Name, "", fmt.Sprintf("+%.0f kW", b.spikeKW), b.spikeUntil, model.SeverityWarning)
	}
	if now.Before(b.spikeUntil) {
		load += b.spikeKW
	}
	kwh := math.Max(load, 0) * step.Hours()
	device := "meter-" + strings.ToLower(b.building.Name)
	return model.EventInput{
		EventID: ids.New("evt"), DeviceID: device, Building: b.building.Name,
		EventType: model.EventEnergy, Value: model.Number(math.Round(kwh*1000) / 1000), Unit: unit("kWh"), Timestamp: ts,
	}
}

func (s *Simulator) reading(device string, r campus.Room, t model.EventType, v float64, ts string) model.EventInput {
	units := map[model.EventType]string{model.EventOccupancy: "people", model.EventTemperature: "celsius", model.EventHumidity: "percent"}
	return model.EventInput{
		EventID: ids.New("evt"), DeviceID: device, Building: r.Building, RoomID: r.ID,
		EventType: t, Value: model.Number(v), Unit: unit(units[t]), Timestamp: ts,
	}
}

func (s *Simulator) state(device string, r campus.Room, t model.EventType, v, ts string) model.EventInput {
	return model.EventInput{
		EventID: ids.New("evt"), DeviceID: device, Building: r.Building, RoomID: r.ID,
		EventType: t, Value: model.Text(v), Timestamp: ts,
	}
}

func (s *Simulator) logAnomaly(now time.Time, kind, building, roomID, value string, until time.Time, expected model.Severity) {
	s.Anomalies++
	if s.opts.AnomalyLog == nil {
		return
	}
	line, _ := json.Marshal(map[string]any{
		"started_at":        model.FormatTime(now),
		"until":             model.FormatTime(until),
		"kind":              kind,
		"building":          building,
		"room_id":           roomID,
		"value":             value,
		"expected_severity": expected,
	})
	_, _ = s.opts.AnomalyLog.Write(append(line, '\n'))
}

func (s *Simulator) pick(kinds []anomalyKind) string {
	total := 0
	for _, k := range kinds {
		total += k.weight
	}
	n := s.rng.IntN(total)
	for _, k := range kinds {
		if n < k.weight {
			return k.name
		}
		n -= k.weight
	}
	return kinds[0].name
}

// RequestTemplate is a typical student service request.
type RequestTemplate struct {
	Category    model.RequestCategory
	Title       string
	Description string // %s is replaced with the room name
	Weight      int
}

var requestTemplates = []RequestTemplate{
	{model.CategoryHeatingCooling, "Heating not working", "Room %s is very cold and the radiator is off.", 4},
	{model.CategoryITSupport, "Wi-Fi down", "No Wi-Fi connection in %s since this morning.", 4},
	{model.CategoryITSupport, "Projector shows no signal", "The projector in %s shows no signal with any laptop.", 3},
	{model.CategoryMaintenance, "Broken chairs", "Two chairs are broken in %s.", 3},
	{model.CategoryMaintenance, "Water dripping from ceiling", "Water is dripping from the ceiling near the window in %s.", 2},
	{model.CategoryCleaning, "Spilled coffee", "Coffee spilled on the floor at the entrance of %s.", 3},
	{model.CategoryAccessBadge, "Badge does not open door", "My student badge does not open the door of %s.", 2},
	{model.CategorySafety, "Emergency exit blocked", "Boxes are blocking the emergency exit in %s.", 1},
	{model.CategoryMedical, "Student feeling unwell", "A student fainted in %s and needs assistance.", 1},
	{model.CategoryOther, "Lost and found", "Found a laptop charger in %s.", 2},
}

// RandomRequest returns a plausible service request for a random room.
func (s *Simulator) RandomRequest() model.NewServiceRequest {
	total := 0
	for _, t := range requestTemplates {
		total += t.Weight
	}
	n := s.rng.IntN(total)
	tmpl := requestTemplates[0]
	for _, t := range requestTemplates {
		if n < t.Weight {
			tmpl = t
			break
		}
		n -= t.Weight
	}
	rooms := s.Rooms()
	room := rooms[s.rng.IntN(len(rooms))]
	return model.NewServiceRequest{
		Title:       tmpl.Title,
		Description: fmt.Sprintf(tmpl.Description, room.Name),
		Category:    tmpl.Category,
		Building:    room.Building,
		RoomID:      room.ID,
	}
}

// occupancyProfile is the expected share of capacity in use at a local hour.
func occupancyProfile(t model.RoomType, h float64, weekend bool) float64 {
	var p float64
	switch t {
	case model.RoomLectureHall:
		p = 0.85*bump(h, 10, 1.3) + 0.75*bump(h, 14.5, 1.3)
	case model.RoomClassroom:
		p = 0.65 * plateau(h, 8, 18, 0.75)
	case model.RoomLab:
		p = 0.55 * plateau(h, 9, 17.5, 0.75)
	case model.RoomLibrary:
		p = 0.35*plateau(h, 8, 23, 1) + 0.4*bump(h, 17, 3)
	case model.RoomStudySpace:
		p = 0.3*plateau(h, 8.5, 22.5, 1) + 0.45*bump(h, 15, 3.5)
	}
	if weekend {
		if t == model.RoomLibrary || t == model.RoomStudySpace {
			p *= 0.5
		} else {
			p *= 0.15
		}
	}
	return clamp(p, 0, 0.95)
}

func bump(h, center, width float64) float64 {
	d := (h - center) / width
	return math.Exp(-d * d)
}

// plateau is about 1 between start and end, easing in and out over ramp hours.
func plateau(h, start, end, ramp float64) float64 {
	sigmoid := func(x float64) float64 { return 1 / (1 + math.Exp(-x)) }
	return sigmoid((h-start)/ramp*4) * sigmoid((end-h)/ramp*4)
}

// smoothing is how far a value moves toward its target in one step, for a
// change that takes about tau.
func smoothing(step, tau time.Duration) float64 {
	return 1 - math.Exp(-step.Seconds()/tau.Seconds())
}

func severityAbove(v, criticalAbove float64) model.Severity {
	if v > criticalAbove {
		return model.SeverityCritical
	}
	return model.SeverityWarning
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }
func round1(v float64) float64        { return math.Round(v*10) / 10 }
func unit(s string) *string           { return &s }
