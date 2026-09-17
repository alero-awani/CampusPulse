package rules

import (
	"strings"
	"testing"
	"time"

	"campuspulse/internal/model"
)

func TestRoomStatus(t *testing.T) {
	th := DefaultThresholds().Occupancy
	tests := []struct {
		occupancy, capacity int
		want                model.RoomStatus
	}{
		{0, 40, model.RoomEmpty},
		{10, 40, model.RoomNormal},
		{28, 40, model.RoomBusy},
		{40, 40, model.RoomBusy},
		{41, 40, model.RoomOverloaded},
		{5, 0, model.RoomOverloaded},
	}
	for _, tt := range tests {
		if got := RoomStatus(tt.occupancy, tt.capacity, th); got != tt.want {
			t.Errorf("RoomStatus(%d, %d) = %s, want %s", tt.occupancy, tt.capacity, got, tt.want)
		}
	}
}

func event(typ model.EventType, v model.Value) model.Event {
	room := "library-a-a203"
	return model.Event{EventID: "evt-1", Building: "Library-A", RoomID: &room, EventType: typ, Value: v}
}

func TestEvaluateEvent(t *testing.T) {
	th := DefaultThresholds()
	tests := []struct {
		name     string
		event    model.Event
		want     model.Severity
		contains string
	}{
		{"occupancy under capacity", event(model.EventOccupancy, model.Number(38)), model.SeverityNormal, ""},
		{"occupancy slightly over", event(model.EventOccupancy, model.Number(44)), model.SeverityWarning, "110% of capacity (40)"},
		{"occupancy far over", event(model.EventOccupancy, model.Number(52)), model.SeverityCritical, "130% of capacity"},
		{"comfortable temperature", event(model.EventTemperature, model.Number(22.5)), model.SeverityNormal, ""},
		{"warm room", event(model.EventTemperature, model.Number(28.4)), model.SeverityWarning, "above the warning limit of 27 °C"},
		{"overheating", event(model.EventTemperature, model.Number(38.2)), model.SeverityCritical, "Temperature 38.2 °C is above the critical limit of 32 °C"},
		{"cold room", event(model.EventTemperature, model.Number(12)), model.SeverityCritical, "below the critical limit"},
		{"humid room", event(model.EventHumidity, model.Number(65)), model.SeverityWarning, "Humidity 65%"},
		{"door closed", event(model.EventDoor, model.Text("closed")), model.SeverityNormal, ""},
		{"door forced", event(model.EventDoor, model.Text("forced_open")), model.SeverityCritical, "Door forced open"},
		{"equipment ok", event(model.EventEquipment, model.Text("ok")), model.SeverityNormal, ""},
		{"projector", event(model.EventEquipment, model.Text("projector_failure")), model.SeverityWarning, "Projector failure"},
		{"leak", event(model.EventEquipment, model.Text("water_leak")), model.SeverityCritical, "Water leak detected"},
		{"unknown fault", event(model.EventEquipment, model.Text("printer_jam")), model.SeverityWarning, "Printer jam reported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sev, f := EvaluateEvent(tt.event, 40, th)
			if sev != tt.want {
				t.Fatalf("severity = %s, want %s", sev, tt.want)
			}
			if tt.want == model.SeverityNormal {
				if f != nil {
					t.Fatalf("unexpected finding %+v", f)
				}
				return
			}
			if f == nil || !strings.Contains(f.Message, tt.contains) {
				t.Fatalf("finding = %+v, want message containing %q", f, tt.contains)
			}
		})
	}
}

func TestDedupKeySeparatesProblems(t *testing.T) {
	th := DefaultThresholds()
	_, hot := EvaluateEvent(event(model.EventTemperature, model.Number(35)), 40, th)
	_, hotter := EvaluateEvent(event(model.EventTemperature, model.Number(37)), 40, th)
	_, leak := EvaluateEvent(event(model.EventEquipment, model.Text("water_leak")), 40, th)
	_, projector := EvaluateEvent(event(model.EventEquipment, model.Text("projector_failure")), 40, th)
	if hot.DedupKey != hotter.DedupKey {
		t.Errorf("two temperature readings in one room should share an alert")
	}
	if leak.DedupKey == projector.DedupKey {
		t.Errorf("different equipment faults should raise separate alerts")
	}
}

func TestEvaluateEnergyHour(t *testing.T) {
	e := DefaultThresholds().Energy
	if f := EvaluateEnergyHour("Engineering-B", 14, 120, e); f != nil {
		t.Errorf("120 kWh should be normal, got %+v", f)
	}
	if f := EvaluateEnergyHour("Engineering-B", 14, 180, e); f == nil || f.Severity != model.SeverityWarning {
		t.Errorf("180 kWh should be a warning, got %+v", f)
	}
	f := EvaluateEnergyHour("Engineering-B", 23, 260, e)
	if f == nil || f.Severity != model.SeverityCritical || !strings.Contains(f.Message, "between 23:00 and 00:00") {
		t.Errorf("260 kWh at 23:00 should be critical, got %+v", f)
	}
}

func TestPriority(t *testing.T) {
	urgent := DefaultThresholds().ServiceRequests.UrgentCategories
	tests := []struct {
		category    model.RequestCategory
		title, desc string
		want        model.Priority
	}{
		{model.CategoryMedical, "Student unwell", "Needs help", model.PriorityUrgent},
		{model.CategoryCleaning, "Smell", "There is smoke coming from the bin", model.PriorityUrgent},
		{model.CategoryHeatingCooling, "Cold", "Radiator off", model.PriorityHigh},
		{model.CategoryITSupport, "Wi-Fi", "No connection", model.PriorityMedium},
		{model.CategoryITSupport, "Firewall", "Firewall blocks the lab printer", model.PriorityMedium},
		{model.CategoryOther, "Lost charger", "Found in A203", model.PriorityLow},
	}
	for _, tt := range tests {
		if got := Priority(tt.category, tt.title, tt.desc, urgent); got != tt.want {
			t.Errorf("Priority(%s, %q) = %s, want %s", tt.category, tt.desc, got, tt.want)
		}
	}
}

func TestEscalationReason(t *testing.T) {
	created := time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC)
	hour := time.Hour
	tests := []struct {
		name     string
		status   model.RequestStatus
		priority model.Priority
		now      time.Time
		want     bool
	}{
		{"urgent, just created", model.RequestOpen, model.PriorityUrgent, created.Add(10 * time.Minute), false},
		{"high, untouched for an hour", model.RequestOpen, model.PriorityHigh, created.Add(hour), true},
		{"high, being worked on", model.RequestInProgress, model.PriorityHigh, created.Add(2 * hour), false},
		{"low, untouched for an hour", model.RequestOpen, model.PriorityLow, created.Add(2 * hour), false},
		{"low, past due", model.RequestInProgress, model.PriorityLow, created.Add(73 * hour), true},
		{"resolved, past due", model.RequestResolved, model.PriorityUrgent, created.Add(5 * hour), false},
	}
	for _, tt := range tests {
		due := created.Add(ResponseTime(tt.priority))
		if _, got := EscalationReason(tt.status, tt.priority, created, due, tt.now, hour); got != tt.want {
			t.Errorf("%s: escalate = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestValidateThresholds(t *testing.T) {
	if err := ValidateThresholds(DefaultThresholds()); err != nil {
		t.Fatalf("defaults should be valid: %v", err)
	}
	bad := []func(*model.ThresholdRules){
		func(r *model.ThresholdRules) { r.Occupancy.BusyRatio = 1.2 },
		func(r *model.ThresholdRules) { r.Temperature.WarningMax = 40 },
		func(r *model.ThresholdRules) { r.Humidity.CriticalMax = 120; r.Humidity.WarningMax = 110 },
		func(r *model.ThresholdRules) { r.Energy.CriticalKWhPerHour = 100 },
		func(r *model.ThresholdRules) { r.ServiceRequests.EscalateAfterMinutes = 0 },
		func(r *model.ThresholdRules) { r.ServiceRequests.UrgentCategories = []model.RequestCategory{"parking"} },
	}
	for i, mutate := range bad {
		r := DefaultThresholds()
		mutate(&r)
		if err := ValidateThresholds(r); err == nil {
			t.Errorf("case %d: expected a validation error", i)
		}
	}
}
