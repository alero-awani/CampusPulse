// Package rules holds the processing logic that turns raw readings into
// decisions: room status, event severity, alerts, request priority, and escalation.
// It has no I/O, so every rule is unit tested.
package rules

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"campuspulse/internal/apperr"
	"campuspulse/internal/model"
)

// DefaultThresholds are used until an admin saves their own.
func DefaultThresholds() model.ThresholdRules {
	return model.ThresholdRules{
		Occupancy:   model.OccupancyThresholds{BusyRatio: 0.7, OverloadedRatio: 1.0},
		Temperature: model.ThresholdRange{WarningMin: 18, WarningMax: 27, CriticalMin: 15, CriticalMax: 32},
		Humidity:    model.ThresholdRange{WarningMin: 30, WarningMax: 60, CriticalMin: 20, CriticalMax: 75},
		Energy:      model.EnergyThresholds{WarningKWhPerHour: 150, CriticalKWhPerHour: 220},
		ServiceRequests: model.RequestThresholds{
			UrgentCategories:     []model.RequestCategory{model.CategorySafety, model.CategoryMedical},
			EscalateAfterMinutes: 60,
		},
	}
}

// ValidateThresholds checks that the rules are internally consistent.
func ValidateThresholds(t model.ThresholdRules) error {
	o := t.Occupancy
	if o.BusyRatio <= 0 || o.BusyRatio >= o.OverloadedRatio || o.OverloadedRatio > 5 {
		return apperr.Invalid("occupancy: busy_ratio must be above 0 and below overloaded_ratio, and overloaded_ratio at most 5")
	}
	if err := validateRange("temperature", t.Temperature, -50, 100); err != nil {
		return err
	}
	if err := validateRange("humidity", t.Humidity, 0, 100); err != nil {
		return err
	}
	if e := t.Energy; e.WarningKWhPerHour <= 0 || e.WarningKWhPerHour >= e.CriticalKWhPerHour {
		return apperr.Invalid("energy: warning_kwh_per_hour must be above 0 and below critical_kwh_per_hour")
	}
	sr := t.ServiceRequests
	if sr.EscalateAfterMinutes < 1 || sr.EscalateAfterMinutes > 7*24*60 {
		return apperr.Invalid("service_requests: escalate_after_minutes must be between 1 and 10080")
	}
	for i, c := range sr.UrgentCategories {
		if !model.Valid(c, model.RequestCategories) {
			return apperr.Invalid("service_requests: unknown category %q", c)
		}
		if slices.Contains(sr.UrgentCategories[:i], c) {
			return apperr.Invalid("service_requests: category %q is listed twice", c)
		}
	}
	return nil
}

func validateRange(name string, r model.ThresholdRange, lo, hi float64) error {
	if !(r.CriticalMin <= r.WarningMin && r.WarningMin < r.WarningMax && r.WarningMax <= r.CriticalMax) {
		return apperr.Invalid("%s: limits must satisfy critical_min ≤ warning_min < warning_max ≤ critical_max", name)
	}
	if r.CriticalMin < lo || r.CriticalMax > hi {
		return apperr.Invalid("%s: limits must be between %g and %g", name, lo, hi)
	}
	return nil
}

// RoomStatus classifies a room by how full it is.
func RoomStatus(occupancy, capacity int, t model.OccupancyThresholds) model.RoomStatus {
	if occupancy <= 0 {
		return model.RoomEmpty
	}
	if capacity <= 0 {
		return model.RoomOverloaded
	}
	switch u := float64(occupancy) / float64(capacity); {
	case u > t.OverloadedRatio:
		return model.RoomOverloaded
	case u >= t.BusyRatio:
		return model.RoomBusy
	}
	return model.RoomNormal
}

// criticalOverloadFactor makes an overload critical once occupancy passes the
// overloaded limit by this factor (for example 120% of capacity with the defaults).
const criticalOverloadFactor = 1.2

// Finding describes an abnormal reading that should raise or update an alert.
type Finding struct {
	Severity  model.Severity
	Threshold *float64
	Message   string
	// DedupKey identifies "the same problem": while an alert with this key is not
	// resolved, new findings update it instead of creating another alert.
	DedupKey string
}

var criticalEquipment = []string{"fire_alarm", "smoke_detected", "water_leak"}

var equipmentLabels = map[string]string{
	"projector_failure":   "Projector failure",
	"smart_board_offline": "Smart board offline",
	"hvac_fault":          "HVAC fault",
	"network_down":        "Network down",
	"fire_alarm":          "Fire alarm triggered",
	"smoke_detected":      "Smoke detected",
	"water_leak":          "Water leak detected",
}

// EvaluateEvent returns the severity of one event and, if it is abnormal, the
// alert it should raise. Energy readings are judged per hour by EvaluateEnergyHour.
func EvaluateEvent(e model.Event, capacity int, t model.ThresholdRules) (model.Severity, *Finding) {
	location := "building:" + e.Building
	if e.RoomID != nil {
		location = *e.RoomID
	}
	key := location + "|" + string(e.EventType)
	num, _ := e.Value.Number()
	text, _ := e.Value.Text()

	switch e.EventType {
	case model.EventOccupancy:
		limit := float64(capacity) * t.Occupancy.OverloadedRatio
		if capacity <= 0 || num <= limit {
			return model.SeverityNormal, nil
		}
		sev := model.SeverityWarning
		if num > limit*criticalOverloadFactor {
			sev = model.SeverityCritical
		}
		msg := fmt.Sprintf("Occupancy %s is %.0f%% of capacity (%d)", formatNumber(num), num/float64(capacity)*100, capacity)
		return sev, &Finding{Severity: sev, Threshold: &limit, Message: msg, DedupKey: key}

	case model.EventTemperature:
		return evaluateRange("Temperature", " °C", num, t.Temperature, key)

	case model.EventHumidity:
		return evaluateRange("Humidity", "%", num, t.Humidity, key)

	case model.EventDoor:
		switch text {
		case "forced_open":
			return model.SeverityCritical, &Finding{Severity: model.SeverityCritical, Message: "Door forced open", DedupKey: key + "|" + text}
		case "held_open":
			return model.SeverityWarning, &Finding{Severity: model.SeverityWarning, Message: "Door held open", DedupKey: key + "|" + text}
		}
		return model.SeverityNormal, nil

	case model.EventEquipment:
		if text == "ok" {
			return model.SeverityNormal, nil
		}
		sev := model.SeverityWarning
		if slices.Contains(criticalEquipment, text) {
			sev = model.SeverityCritical
		}
		label, ok := equipmentLabels[text]
		if !ok {
			label = humanize(text) + " reported"
		}
		return sev, &Finding{Severity: sev, Message: label, DedupKey: key + "|" + text}
	}
	return model.SeverityNormal, nil
}

func evaluateRange(label, unit string, v float64, r model.ThresholdRange, key string) (model.Severity, *Finding) {
	var (
		sev   model.Severity
		limit float64
		how   string
	)
	switch {
	case v > r.CriticalMax:
		sev, limit, how = model.SeverityCritical, r.CriticalMax, "above the critical limit"
	case v < r.CriticalMin:
		sev, limit, how = model.SeverityCritical, r.CriticalMin, "below the critical limit"
	case v > r.WarningMax:
		sev, limit, how = model.SeverityWarning, r.WarningMax, "above the warning limit"
	case v < r.WarningMin:
		sev, limit, how = model.SeverityWarning, r.WarningMin, "below the warning limit"
	default:
		return model.SeverityNormal, nil
	}
	msg := fmt.Sprintf("%s %s%s is %s of %s%s", label, formatNumber(v), unit, how, formatNumber(limit), unit)
	return sev, &Finding{Severity: sev, Threshold: &limit, Message: msg, DedupKey: key}
}

// EvaluateEnergyHour checks a building's running energy total for one hour.
func EvaluateEnergyHour(building string, hour int, kwh float64, t model.EnergyThresholds) *Finding {
	sev, limit := model.SeverityCritical, t.CriticalKWhPerHour
	if kwh <= limit {
		sev, limit = model.SeverityWarning, t.WarningKWhPerHour
		if kwh <= limit {
			return nil
		}
	}
	msg := fmt.Sprintf("%s used %s kWh between %02d:00 and %02d:00, above the %s limit of %s kWh",
		building, formatNumber(kwh), hour, (hour+1)%24, sev, formatNumber(limit))
	return &Finding{Severity: sev, Threshold: &limit, Message: msg, DedupKey: "building:" + building + "|energy"}
}

var urgentWords = regexp.MustCompile(`\b(fire|smoke|bleeding|injur\w*|unconscious|faint\w*|collaps\w*|flood\w*|gas leak|electric shock|emergency)\b`)

// Priority decides how urgent a service request is. Students never choose it.
func Priority(c model.RequestCategory, title, description string, urgentCategories []model.RequestCategory) model.Priority {
	if slices.Contains(urgentCategories, c) || urgentWords.MatchString(strings.ToLower(title+" "+description)) {
		return model.PriorityUrgent
	}
	switch c {
	case model.CategorySafety, model.CategoryMedical, model.CategoryHeatingCooling, model.CategoryAccessBadge:
		return model.PriorityHigh
	case model.CategoryMaintenance, model.CategoryITSupport:
		return model.PriorityMedium
	}
	return model.PriorityLow
}

// ResponseTime is how long staff have to resolve a request of each priority.
func ResponseTime(p model.Priority) time.Duration {
	switch p {
	case model.PriorityUrgent:
		return time.Hour
	case model.PriorityHigh:
		return 4 * time.Hour
	case model.PriorityMedium:
		return 24 * time.Hour
	}
	return 72 * time.Hour
}

// EscalationReason reports whether an unresolved request should be escalated
// automatically, and why: it is past its due time, or it is high or urgent
// priority and nobody has started on it within escalateAfter.
func EscalationReason(status model.RequestStatus, p model.Priority, createdAt, dueAt, now time.Time, escalateAfter time.Duration) (string, bool) {
	if status == model.RequestResolved {
		return "", false
	}
	if now.After(dueAt) {
		return "Not resolved by its due time", true
	}
	if status == model.RequestOpen && p.Rank() >= model.PriorityHigh.Rank() && now.Sub(createdAt) >= escalateAfter {
		return fmt.Sprintf("Still open after %d minutes", int(escalateAfter.Minutes())), true
	}
	return "", false
}

func formatNumber(v float64) string {
	return strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64)
}

// humanize turns "projector_failure" into "Projector failure".
func humanize(code string) string {
	s := strings.ReplaceAll(code, "_", " ")
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
