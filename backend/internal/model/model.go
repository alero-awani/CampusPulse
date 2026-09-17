// Package model defines the JSON types of the CampusPulse API contract
// (docs/lovable-prompt.md). Field names and enum values must stay in sync with it.
package model

import (
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"
)

// TimeLayout is the timestamp format used in responses and storage. It is fixed
// width, so stored timestamps sort correctly as strings.
const TimeLayout = "2006-01-02T15:04:05.000Z"

// FormatTime formats t in UTC using TimeLayout.
func FormatTime(t time.Time) string { return t.UTC().Format(TimeLayout) }

type Role string

const (
	RoleStudent Role = "student"
	RoleStaff   Role = "staff"
	RoleAdmin   Role = "admin"
)

var Roles = []Role{RoleStudent, RoleStaff, RoleAdmin}

type Severity string

const (
	SeverityNormal   Severity = "normal"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

var Severities = []Severity{SeverityNormal, SeverityWarning, SeverityCritical}

// Rank orders severities from normal (0) to critical (2).
func (s Severity) Rank() int { return slices.Index(Severities, s) }

type RoomStatus string

const (
	RoomEmpty      RoomStatus = "empty"
	RoomNormal     RoomStatus = "normal"
	RoomBusy       RoomStatus = "busy"
	RoomOverloaded RoomStatus = "overloaded"
)

var RoomStatuses = []RoomStatus{RoomEmpty, RoomNormal, RoomBusy, RoomOverloaded}

type RoomType string

const (
	RoomClassroom   RoomType = "classroom"
	RoomLectureHall RoomType = "lecture_hall"
	RoomLab         RoomType = "lab"
	RoomLibrary     RoomType = "library"
	RoomStudySpace  RoomType = "study_space"
)

var RoomTypes = []RoomType{RoomClassroom, RoomLectureHall, RoomLab, RoomLibrary, RoomStudySpace}

type EventType string

const (
	EventOccupancy   EventType = "occupancy"
	EventTemperature EventType = "temperature"
	EventHumidity    EventType = "humidity"
	EventEnergy      EventType = "energy"
	EventDoor        EventType = "door"
	EventEquipment   EventType = "equipment"
)

var EventTypes = []EventType{EventOccupancy, EventTemperature, EventHumidity, EventEnergy, EventDoor, EventEquipment}

type AlertStatus string

const (
	AlertOpen         AlertStatus = "open"
	AlertAcknowledged AlertStatus = "acknowledged"
	AlertResolved     AlertStatus = "resolved"
)

var AlertStatuses = []AlertStatus{AlertOpen, AlertAcknowledged, AlertResolved}

type RequestStatus string

const (
	RequestOpen       RequestStatus = "open"
	RequestInProgress RequestStatus = "in_progress"
	RequestResolved   RequestStatus = "resolved"
)

var RequestStatuses = []RequestStatus{RequestOpen, RequestInProgress, RequestResolved}

type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityMedium Priority = "medium"
	PriorityHigh   Priority = "high"
	PriorityUrgent Priority = "urgent"
)

var Priorities = []Priority{PriorityLow, PriorityMedium, PriorityHigh, PriorityUrgent}

// Rank orders priorities from low (0) to urgent (3).
func (p Priority) Rank() int { return slices.Index(Priorities, p) }

type RequestCategory string

const (
	CategoryMaintenance    RequestCategory = "maintenance"
	CategoryITSupport      RequestCategory = "it_support"
	CategoryCleaning       RequestCategory = "cleaning"
	CategoryHeatingCooling RequestCategory = "heating_cooling"
	CategoryAccessBadge    RequestCategory = "access_badge"
	CategorySafety         RequestCategory = "safety"
	CategoryMedical        RequestCategory = "medical"
	CategoryOther          RequestCategory = "other"
)

var RequestCategories = []RequestCategory{
	CategoryMaintenance, CategoryITSupport, CategoryCleaning, CategoryHeatingCooling,
	CategoryAccessBadge, CategorySafety, CategoryMedical, CategoryOther,
}

// Valid reports whether v is one of the allowed enum values.
func Valid[T comparable](v T, allowed []T) bool { return slices.Contains(allowed, v) }

// JoinEnum lists enum values for error messages.
func JoinEnum[T ~string](values []T) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = string(v)
	}
	return strings.Join(parts, ", ")
}

// Value is an event reading: a number for measurements, a string for door and
// equipment states. The zero Value means the field was missing or null.
type Value struct {
	num *float64
	str *string
}

func Number(f float64) Value { return Value{num: &f} }
func Text(s string) Value    { return Value{str: &s} }

func (v Value) IsSet() bool { return v.num != nil || v.str != nil }

func (v Value) Number() (float64, bool) {
	if v.num == nil {
		return 0, false
	}
	return *v.num, true
}

func (v Value) Text() (string, bool) {
	if v.str == nil {
		return "", false
	}
	return *v.str, true
}

func (v Value) String() string {
	if v.num != nil {
		return strconv.FormatFloat(*v.num, 'f', -1, 64)
	}
	if v.str != nil {
		return *v.str
	}
	return ""
}

func (v Value) MarshalJSON() ([]byte, error) {
	switch {
	case v.num != nil:
		return json.Marshal(*v.num)
	case v.str != nil:
		return json.Marshal(*v.str)
	}
	return []byte("null"), nil
}

func (v *Value) UnmarshalJSON(b []byte) error {
	*v = Value{}
	if string(b) == "null" {
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err == nil {
		v.num = &f
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		v.str = &s
		return nil
	}
	return errors.New("value must be a number or a string")
}

// Optional tells a missing JSON field apart from an explicit null, which PATCH
// bodies need ("assigned_to": null clears the assignee, a missing field keeps it).
type Optional[T any] struct {
	Set   bool
	Value *T
}

func (o *Optional[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	o.Value = nil
	if string(b) == "null" {
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.Value = &v
	return nil
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

type Page[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

type List[T any] struct {
	Items []T `json:"items"`
}

type Health struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Time    string `json:"time"`
}

type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  Role   `json:"role"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
	User      User   `json:"user"`
}

type RoomCounts struct {
	Total      int `json:"total"`
	Occupied   int `json:"occupied"`
	Busy       int `json:"busy"`
	Overloaded int `json:"overloaded"`
}

type AlertCounts struct {
	Open     int `json:"open"`
	Critical int `json:"critical"`
	Warning  int `json:"warning"`
}

type RequestCounts struct {
	Open      int `json:"open"`
	Urgent    int `json:"urgent"`
	Escalated int `json:"escalated"`
}

type TopBuilding struct {
	Building string  `json:"building"`
	KWh      float64 `json:"kwh"`
}

type EnergyToday struct {
	TotalKWh    float64      `json:"total_kwh"`
	TopBuilding *TopBuilding `json:"top_building"`
}

type Ingestion struct {
	EventsLastHour  int     `json:"events_last_hour"`
	EventsPerMinute float64 `json:"events_per_minute"`
}

type Stats struct {
	GeneratedAt     string        `json:"generated_at"`
	Rooms           RoomCounts    `json:"rooms"`
	Alerts          AlertCounts   `json:"alerts"`
	ServiceRequests RequestCounts `json:"service_requests"`
	EnergyToday     EnergyToday   `json:"energy_today"`
	Ingestion       Ingestion     `json:"ingestion"`
}

type BuildingEnergy struct {
	Building  string    `json:"building"`
	TotalKWh  float64   `json:"total_kwh"`
	Share     float64   `json:"share"`
	PeakHour  int       `json:"peak_hour"`
	PeakKWh   float64   `json:"peak_kwh"`
	HourlyKWh []float64 `json:"hourly_kwh"`
}

type EnergyStats struct {
	Date      string           `json:"date"`
	TotalKWh  float64          `json:"total_kwh"`
	Buildings []BuildingEnergy `json:"buildings"`
}

type Room struct {
	RoomID      string     `json:"room_id"`
	Name        string     `json:"name"`
	Building    string     `json:"building"`
	Floor       int        `json:"floor"`
	Type        RoomType   `json:"type"`
	Capacity    int        `json:"capacity"`
	Occupancy   int        `json:"occupancy"`
	Utilization float64    `json:"utilization"`
	Status      RoomStatus `json:"status"`
	Temperature *float64   `json:"temperature"`
	Humidity    *float64   `json:"humidity"`
	OpenAlerts  int        `json:"open_alerts"`
	LastUpdated string     `json:"last_updated"`
}

type RoomReading struct {
	Timestamp   string   `json:"timestamp"`
	Occupancy   *float64 `json:"occupancy"`
	Temperature *float64 `json:"temperature"`
	Humidity    *float64 `json:"humidity"`
}

type RoomDetail struct {
	Room         Room          `json:"room"`
	Readings     []RoomReading `json:"readings"`
	RecentEvents []Event       `json:"recent_events"`
}

type Event struct {
	EventID   string    `json:"event_id"`
	DeviceID  string    `json:"device_id"`
	Building  string    `json:"building"`
	RoomID    *string   `json:"room_id"`
	Room      *string   `json:"room"`
	EventType EventType `json:"event_type"`
	Value     Value     `json:"value"`
	Unit      *string   `json:"unit"`
	Severity  Severity  `json:"severity"`
	Timestamp string    `json:"timestamp"`
}

// EventInput is what sensors send. It accepts the Appendix A format: a room can be
// given by room_id or by its name in "room", and "severity" is ignored because the
// backend computes it.
type EventInput struct {
	EventID   string    `json:"event_id"`
	DeviceID  string    `json:"device_id"`
	Building  string    `json:"building"`
	RoomID    string    `json:"room_id,omitempty"`
	Room      string    `json:"room,omitempty"`
	EventType EventType `json:"event_type"`
	Value     Value     `json:"value"`
	Unit      *string   `json:"unit,omitempty"`
	Timestamp string    `json:"timestamp"`
	Severity  string    `json:"severity,omitempty"`
}

type IngestStatus string

const (
	IngestStored    IngestStatus = "stored"
	IngestDuplicate IngestStatus = "duplicate"
	IngestRejected  IngestStatus = "rejected"
	IngestFailed    IngestStatus = "failed"
)

type IngestResult struct {
	EventID  string       `json:"event_id"`
	Status   IngestStatus `json:"status"`
	Severity Severity     `json:"severity,omitempty"`
	Error    string       `json:"error,omitempty"`
}

type BatchRequest struct {
	Events []EventInput `json:"events"`
}

type BatchResponse struct {
	Accepted   int            `json:"accepted"`
	Duplicates int            `json:"duplicates"`
	Rejected   int            `json:"rejected"`
	Failed     int            `json:"failed"`
	Results    []IngestResult `json:"results"`
}

type Alert struct {
	AlertID   string      `json:"alert_id"`
	EventID   string      `json:"event_id"`
	Building  string      `json:"building"`
	RoomID    *string     `json:"room_id"`
	Room      *string     `json:"room"`
	Type      EventType   `json:"type"`
	Severity  Severity    `json:"severity"`
	Message   string      `json:"message"`
	Value     Value       `json:"value"`
	Unit      *string     `json:"unit"`
	Threshold *float64    `json:"threshold"`
	Status    AlertStatus `json:"status"`
	CreatedAt string      `json:"created_at"`
	UpdatedAt string      `json:"updated_at"`
	UpdatedBy *string     `json:"updated_by"`
}

type AlertUpdate struct {
	Status AlertStatus `json:"status"`
	Note   string      `json:"note,omitempty"`
}

const (
	HistoryCreated       = "created"
	HistoryStatusChanged = "status_changed"
	HistoryAssigned      = "assigned"
	HistoryEscalated     = "escalated"
	HistoryDeEscalated   = "de_escalated"
	HistoryNote          = "note"
)

type RequestHistoryEntry struct {
	At     string  `json:"at"`
	By     string  `json:"by"`
	Action string  `json:"action"`
	Detail *string `json:"detail"`
}

type Person struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ServiceRequest struct {
	RequestID        string                `json:"request_id"`
	Title            string                `json:"title"`
	Description      string                `json:"description"`
	Category         RequestCategory       `json:"category"`
	Building         string                `json:"building"`
	RoomID           *string               `json:"room_id"`
	Room             *string               `json:"room"`
	Priority         Priority              `json:"priority"`
	Status           RequestStatus         `json:"status"`
	Escalated        bool                  `json:"escalated"`
	EscalationReason *string               `json:"escalation_reason"`
	CreatedBy        Person                `json:"created_by"`
	AssignedTo       *string               `json:"assigned_to"`
	CreatedAt        string                `json:"created_at"`
	UpdatedAt        string                `json:"updated_at"`
	DueAt            string                `json:"due_at"`
	History          []RequestHistoryEntry `json:"history,omitempty"`
}

type NewServiceRequest struct {
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Category    RequestCategory `json:"category"`
	Building    string          `json:"building"`
	RoomID      string          `json:"room_id,omitempty"`
}

type ServiceRequestUpdate struct {
	Status     *RequestStatus   `json:"status"`
	AssignedTo Optional[string] `json:"assigned_to"`
	Escalated  *bool            `json:"escalated"`
	Note       *string          `json:"note"`
}

type ThresholdRange struct {
	WarningMin  float64 `json:"warning_min"`
	WarningMax  float64 `json:"warning_max"`
	CriticalMin float64 `json:"critical_min"`
	CriticalMax float64 `json:"critical_max"`
}

type OccupancyThresholds struct {
	BusyRatio       float64 `json:"busy_ratio"`
	OverloadedRatio float64 `json:"overloaded_ratio"`
}

type EnergyThresholds struct {
	WarningKWhPerHour  float64 `json:"warning_kwh_per_hour"`
	CriticalKWhPerHour float64 `json:"critical_kwh_per_hour"`
}

type RequestThresholds struct {
	UrgentCategories     []RequestCategory `json:"urgent_categories"`
	EscalateAfterMinutes int               `json:"escalate_after_minutes"`
}

// ThresholdRules is the editable part of Thresholds (the PUT /thresholds body).
type ThresholdRules struct {
	Occupancy       OccupancyThresholds `json:"occupancy"`
	Temperature     ThresholdRange      `json:"temperature"`
	Humidity        ThresholdRange      `json:"humidity"`
	Energy          EnergyThresholds    `json:"energy"`
	ServiceRequests RequestThresholds   `json:"service_requests"`
}

type Thresholds struct {
	ThresholdRules
	UpdatedAt string `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`
}
