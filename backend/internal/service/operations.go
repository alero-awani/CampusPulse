package service

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"campuspulse/internal/apperr"
	"campuspulse/internal/ids"
	"campuspulse/internal/model"
	"campuspulse/internal/rules"
	"campuspulse/internal/store"
)

// Login signs a user in and returns a session token.
func (s *Service) Login(ctx context.Context, email, password string) (model.LoginResponse, error) {
	return s.auth.Login(ctx, email, password)
}

// Identify returns the signed-in user who made the request.
func (s *Service) Identify(r *http.Request) (model.User, error) {
	return s.auth.Identify(r)
}

type EventQuery = store.EventFilter

func (s *Service) ListEvents(ctx context.Context, f EventQuery) (model.Page[model.Event], error) {
	f.Now = s.Now()
	events, next, err := s.store.ListEvents(ctx, f)
	return model.Page[model.Event]{Items: nonNil(events), NextCursor: next}, err
}

type AlertQuery = store.AlertFilter

func (s *Service) ListAlerts(ctx context.Context, f AlertQuery) (model.Page[model.Alert], error) {
	records, next, err := s.store.ListAlerts(ctx, f)
	if err != nil {
		return model.Page[model.Alert]{}, err
	}
	alerts := make([]model.Alert, len(records))
	for i, r := range records {
		alerts[i] = r.ToModel()
	}
	return model.Page[model.Alert]{Items: alerts, NextCursor: next}, nil
}

// UpdateAlert acknowledges or resolves an alert. Allowed changes are
// open -> acknowledged, open -> resolved, and acknowledged -> resolved.
func (s *Service) UpdateAlert(ctx context.Context, id string, upd model.AlertUpdate, u model.User) (model.Alert, error) {
	if upd.Status != model.AlertAcknowledged && upd.Status != model.AlertResolved {
		return model.Alert{}, apperr.Invalid("status must be acknowledged or resolved")
	}
	note := strings.TrimSpace(upd.Note)
	if utf8.RuneCountInString(note) > 500 {
		return model.Alert{}, apperr.Invalid("note must be at most 500 characters")
	}
	current, err := s.store.GetAlert(ctx, id)
	if err != nil {
		return model.Alert{}, err
	}
	if current == nil {
		return model.Alert{}, apperr.NotFound("alert")
	}
	switch {
	case current.Status == string(upd.Status):
		return current.ToModel(), nil
	case current.Status == string(model.AlertResolved):
		return model.Alert{}, apperr.Conflict("This alert is already resolved")
	}
	updated, err := s.store.SetAlertStatus(ctx, *current, upd.Status, store.Note{
		At: model.FormatTime(s.Now()), By: u.Name, Action: string(upd.Status), Note: note,
	})
	if err != nil {
		return model.Alert{}, conflictOr(err, "This alert was changed by someone else. Reload and try again.")
	}
	return updated.ToModel(), nil
}

func requestModel(r store.RequestRecord, withHistory bool) model.ServiceRequest {
	sr := model.ServiceRequest{
		RequestID:        r.RequestID,
		Title:            r.Title,
		Description:      r.Description,
		Category:         r.Category,
		Building:         r.Building,
		RoomID:           strPtr(r.RoomID),
		Room:             strPtr(r.Room),
		Priority:         r.Priority,
		Status:           r.Status,
		Escalated:        r.Escalated,
		EscalationReason: strPtr(r.EscalationReason),
		CreatedBy:        model.Person{ID: r.CreatorID, Name: r.CreatorName},
		AssignedTo:       strPtr(r.AssignedTo),
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
		DueAt:            r.DueAt,
	}
	if withHistory {
		sr.History = make([]model.RequestHistoryEntry, len(r.History))
		for i, h := range r.History {
			sr.History[i] = model.RequestHistoryEntry{At: h.At, By: h.By, Action: h.Action, Detail: strPtr(h.Detail)}
		}
	}
	return sr
}

// CreateRequest files a student's service request. The server sets the priority
// and due time.
func (s *Service) CreateRequest(ctx context.Context, in model.NewServiceRequest, u model.User) (model.ServiceRequest, error) {
	title, description := strings.TrimSpace(in.Title), strings.TrimSpace(in.Description)
	switch {
	case title == "" || utf8.RuneCountInString(title) > 80:
		return model.ServiceRequest{}, apperr.Invalid("title is required and must be at most 80 characters")
	case description == "" || utf8.RuneCountInString(description) > 1000:
		return model.ServiceRequest{}, apperr.Invalid("description is required and must be at most 1000 characters")
	case !model.Valid(in.Category, model.RequestCategories):
		return model.ServiceRequest{}, apperr.Invalid("category must be one of: %s", model.JoinEnum(model.RequestCategories))
	}
	layout, err := s.campusLayout(ctx)
	if err != nil {
		return model.ServiceRequest{}, err
	}
	if !layout.hasBuilding(in.Building) {
		return model.ServiceRequest{}, apperr.Invalid("unknown building %q", in.Building)
	}
	th, err := s.currentThresholds(ctx)
	if err != nil {
		return model.ServiceRequest{}, err
	}

	now := s.Now()
	priority := rules.Priority(in.Category, title, description, th.ServiceRequests.UrgentCategories)
	rec := store.RequestRecord{
		RequestID:   ids.New("req"),
		Title:       title,
		Description: description,
		Category:    in.Category,
		Building:    in.Building,
		Priority:    priority,
		Status:      model.RequestOpen,
		CreatorID:   u.ID,
		CreatorName: u.Name,
		CreatedAt:   model.FormatTime(now),
		UpdatedAt:   model.FormatTime(now),
		DueAt:       model.FormatTime(now.Add(rules.ResponseTime(priority))),
		History: []store.HistoryRecord{{
			At: model.FormatTime(now), By: u.Name, Action: model.HistoryCreated, Detail: "Priority set to " + string(priority),
		}},
	}
	if in.RoomID != "" {
		room, ok := layout.rooms[in.RoomID]
		if !ok || room.Building != in.Building {
			return model.ServiceRequest{}, apperr.Invalid("room %q is not in building %q", in.RoomID, in.Building)
		}
		rec.RoomID, rec.Room = room.RoomID, room.Name
	}
	if err := s.store.CreateRequest(ctx, rec); err != nil {
		return model.ServiceRequest{}, err
	}
	s.metrics.Count("ServiceRequestsCreated", 1)
	return requestModel(rec, true), nil
}

type RequestQuery = store.RequestFilter

// ListRequests returns service requests. Students only get their own.
func (s *Service) ListRequests(ctx context.Context, f RequestQuery, u model.User) (model.Page[model.ServiceRequest], error) {
	if !isStaff(u) {
		f.CreatorID = u.ID
	}
	if err := s.validBuilding(ctx, f.Building); err != nil {
		return model.Page[model.ServiceRequest]{}, err
	}
	records, next, err := s.store.ListRequests(ctx, f)
	if err != nil {
		return model.Page[model.ServiceRequest]{}, err
	}
	items := make([]model.ServiceRequest, len(records))
	for i, r := range records {
		items[i] = requestModel(r, false)
	}
	return model.Page[model.ServiceRequest]{Items: items, NextCursor: next}, nil
}

// GetRequest returns one request with its history. Students get "not found" for
// other students' requests, so request IDs can't be probed.
func (s *Service) GetRequest(ctx context.Context, id string, u model.User) (model.ServiceRequest, error) {
	rec, err := s.store.GetRequest(ctx, id)
	if err != nil {
		return model.ServiceRequest{}, err
	}
	if rec == nil || (!isStaff(u) && rec.CreatorID != u.ID) {
		return model.ServiceRequest{}, apperr.NotFound("service request")
	}
	return requestModel(*rec, true), nil
}

// UpdateRequest lets staff change status, assignee, and escalation, or add a
// note. Every change is recorded in the request's history.
func (s *Service) UpdateRequest(ctx context.Context, id string, upd model.ServiceRequestUpdate, u model.User) (model.ServiceRequest, error) {
	rec, err := s.store.GetRequest(ctx, id)
	if err != nil {
		return model.ServiceRequest{}, err
	}
	if rec == nil {
		return model.ServiceRequest{}, apperr.NotFound("service request")
	}
	now := model.FormatTime(s.Now())
	changes := store.RequestChanges{UpdatedAt: now}
	addHistory := func(action, detail string) {
		changes.History = append(changes.History, store.HistoryRecord{At: now, By: u.Name, Action: action, Detail: detail})
	}

	if upd.Status != nil && *upd.Status != rec.Status {
		if !model.Valid(*upd.Status, model.RequestStatuses) {
			return model.ServiceRequest{}, apperr.Invalid("status must be one of: %s", model.JoinEnum(model.RequestStatuses))
		}
		changes.Status = upd.Status
		addHistory(model.HistoryStatusChanged, string(rec.Status)+" → "+string(*upd.Status))
	}
	if upd.AssignedTo.Set {
		assignee := ""
		if upd.AssignedTo.Value != nil {
			assignee = strings.TrimSpace(*upd.AssignedTo.Value)
		}
		if utf8.RuneCountInString(assignee) > 80 {
			return model.ServiceRequest{}, apperr.Invalid("assigned_to must be at most 80 characters")
		}
		if assignee != rec.AssignedTo {
			changes.AssignedTo = &assignee
			if assignee == "" {
				addHistory(model.HistoryAssigned, "Unassigned")
			} else {
				addHistory(model.HistoryAssigned, "Assigned to "+assignee)
			}
		}
	}
	if upd.Escalated != nil && *upd.Escalated != rec.Escalated {
		changes.Escalated = upd.Escalated
		if *upd.Escalated {
			reason := "Escalated by " + u.Name
			changes.EscalationReason, changes.EscalationSuppressed = &reason, new(false)
			addHistory(model.HistoryEscalated, reason)
		} else {
			// Staff decided this request doesn't need escalation, so the automatic
			// check must not escalate it again.
			changes.EscalationReason, changes.EscalationSuppressed = new(""), new(true)
			addHistory(model.HistoryDeEscalated, "De-escalated by "+u.Name)
		}
	}
	if upd.Note != nil {
		note := strings.TrimSpace(*upd.Note)
		if utf8.RuneCountInString(note) > 1000 {
			return model.ServiceRequest{}, apperr.Invalid("note must be at most 1000 characters")
		}
		if note != "" {
			addHistory(model.HistoryNote, note)
		}
	}
	if len(changes.History) == 0 {
		return model.ServiceRequest{}, apperr.Invalid("nothing to update")
	}

	updated, err := s.store.UpdateRequest(ctx, *rec, changes)
	if err != nil {
		return model.ServiceRequest{}, conflictOr(err, "This request was changed by someone else. Reload and try again.")
	}
	return requestModel(updated, true), nil
}

// EscalateRequests escalates unresolved requests that are overdue, or that are
// high priority and still untouched. It runs on a schedule and returns how many
// requests it escalated.
func (s *Service) EscalateRequests(ctx context.Context) (int, error) {
	th, err := s.currentThresholds(ctx)
	if err != nil {
		return 0, err
	}
	records, err := s.store.UnresolvedRequests(ctx)
	if err != nil {
		return 0, err
	}
	now := s.Now()
	after := time.Duration(th.ServiceRequests.EscalateAfterMinutes) * time.Minute
	escalated := 0
	for _, r := range records {
		if r.Escalated || r.EscalationSuppressed {
			continue
		}
		created, err1 := time.Parse(model.TimeLayout, r.CreatedAt)
		due, err2 := time.Parse(model.TimeLayout, r.DueAt)
		if err1 != nil || err2 != nil {
			continue
		}
		reason, ok := rules.EscalationReason(r.Status, r.Priority, created, due, now, after)
		if !ok {
			continue
		}
		at := model.FormatTime(now)
		_, err = s.store.UpdateRequest(ctx, r, store.RequestChanges{
			Escalated:        new(true),
			EscalationReason: &reason,
			UpdatedAt:        at,
			History:          []store.HistoryRecord{{At: at, By: "System", Action: model.HistoryEscalated, Detail: reason}},
		})
		if err != nil {
			s.log.WarnContext(ctx, "escalate request", "request_id", r.RequestID, "error", err)
			continue
		}
		escalated++
		s.log.InfoContext(ctx, "request escalated", "request_id", r.RequestID, "reason", reason)
	}
	s.metrics.Count("ServiceRequestsEscalated", float64(escalated))
	return escalated, nil
}

// GetThresholds returns the current alert rules.
func (s *Service) GetThresholds(ctx context.Context) (model.Thresholds, error) {
	return s.currentThresholds(ctx)
}

// SetThresholds validates and saves new alert rules.
func (s *Service) SetThresholds(ctx context.Context, in model.ThresholdRules, u model.User) (model.Thresholds, error) {
	if err := rules.ValidateThresholds(in); err != nil {
		return model.Thresholds{}, err
	}
	if in.ServiceRequests.UrgentCategories == nil {
		in.ServiceRequests.UrgentCategories = []model.RequestCategory{}
	}
	t := model.Thresholds{ThresholdRules: in, UpdatedAt: model.FormatTime(s.Now()), UpdatedBy: u.Name}
	if err := s.store.PutThresholds(ctx, t); err != nil {
		return model.Thresholds{}, err
	}
	s.mu.Lock()
	s.thresholds, s.thresholdsExpires = &t, s.Now().Add(cacheTTL)
	s.mu.Unlock()
	return t, nil
}
