package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"campuspulse/internal/apperr"
	"campuspulse/internal/model"
	"campuspulse/internal/service"
)

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, model.Health{Status: "ok", Version: s.version, Time: model.FormatTime(time.Now())})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req model.LoginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.loginLimiter.allow(clientIP(r) + "|" + strings.ToLower(strings.TrimSpace(req.Email))) {
		s.writeError(w, r, apperr.RateLimited())
		return
	}
	resp, err := s.svc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request, u model.User) {
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) postEvent(w http.ResponseWriter, r *http.Request) {
	var in model.EventInput
	if err := decodeJSON(w, r, &in); err != nil {
		s.writeError(w, r, err)
		return
	}
	res := s.svc.Ingest(r.Context(), in)
	switch res.Status {
	case model.IngestStored:
		writeJSON(w, http.StatusCreated, res)
	case model.IngestDuplicate:
		writeJSON(w, http.StatusOK, res)
	case model.IngestRejected:
		s.writeError(w, r, apperr.Invalid("%s", res.Error))
	default:
		writeJSON(w, http.StatusInternalServerError, model.ErrorResponse{Error: model.ErrorBody{Code: "internal", Message: "The event could not be stored. Retry with the same event_id."}})
	}
}

func (s *Server) postEventBatch(w http.ResponseWriter, r *http.Request) {
	var req model.BatchRequest
	if err := decodeJSON(w, r, &req); err != nil {
		s.writeError(w, r, err)
		return
	}
	resp, err := s.svc.IngestBatch(r.Context(), req.Events)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request, _ model.User) {
	stats, err := s.svc.Stats(r.Context(), r.URL.Query().Get("building"))
	respond(s, w, r, stats, err)
}

func (s *Server) energyStats(w http.ResponseWriter, r *http.Request, _ model.User) {
	stats, err := s.svc.EnergyStats(r.Context(), r.URL.Query().Get("date"))
	respond(s, w, r, stats, err)
}

func (s *Server) listRooms(w http.ResponseWriter, r *http.Request, u model.User) {
	f := service.RoomFilter{Building: r.URL.Query().Get("building")}
	var err error
	if f.Status, err = enumParam(r, "status", model.RoomStatuses); err != nil {
		s.writeError(w, r, err)
		return
	}
	if f.Type, err = enumParam(r, "type", model.RoomTypes); err != nil {
		s.writeError(w, r, err)
		return
	}
	rooms, err := s.svc.ListRooms(r.Context(), f, u)
	respond(s, w, r, model.List[model.Room]{Items: rooms}, err)
}

func (s *Server) getRoom(w http.ResponseWriter, r *http.Request, u model.User) {
	hours, err := intParam(r, "hours", 6)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	detail, err := s.svc.RoomDetail(r.Context(), r.PathValue("roomId"), hours, u)
	respond(s, w, r, detail, err)
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request, _ model.User) {
	q := r.URL.Query()
	f := service.EventQuery{Building: q.Get("building"), RoomID: q.Get("room_id"), Cursor: q.Get("cursor")}
	var err error
	if f.Type, err = enumParam(r, "type", model.EventTypes); err == nil {
		if f.Since, err = timeParam(r, "since"); err == nil {
			f.Limit, err = limitParam(r)
		}
	}
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	page, err := s.svc.ListEvents(r.Context(), f)
	respond(s, w, r, page, err)
}

func (s *Server) listAlerts(w http.ResponseWriter, r *http.Request, _ model.User) {
	q := r.URL.Query()
	f := service.AlertQuery{Building: q.Get("building"), Cursor: q.Get("cursor")}
	var err error
	if f.Status, err = enumParam(r, "status", model.AlertStatuses); err == nil {
		if f.Severity, err = enumParam(r, "severity", model.Severities); err == nil {
			if f.Type, err = enumParam(r, "type", model.EventTypes); err == nil {
				f.Limit, err = limitParam(r)
			}
		}
	}
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	page, err := s.svc.ListAlerts(r.Context(), f)
	respond(s, w, r, page, err)
}

func (s *Server) updateAlert(w http.ResponseWriter, r *http.Request, u model.User) {
	var upd model.AlertUpdate
	if err := decodeJSON(w, r, &upd); err != nil {
		s.writeError(w, r, err)
		return
	}
	alert, err := s.svc.UpdateAlert(r.Context(), r.PathValue("alertId"), upd, u)
	respond(s, w, r, alert, err)
}

func (s *Server) listRequests(w http.ResponseWriter, r *http.Request, u model.User) {
	q := r.URL.Query()
	f := service.RequestQuery{Building: q.Get("building"), Cursor: q.Get("cursor")}
	var err error
	for part := range strings.SplitSeq(q.Get("status"), ",") {
		if st := model.RequestStatus(strings.TrimSpace(part)); st != "" {
			if !model.Valid(st, model.RequestStatuses) {
				s.writeError(w, r, apperr.Invalid("status must be a comma-separated list of: %s", model.JoinEnum(model.RequestStatuses)))
				return
			}
			f.Statuses = append(f.Statuses, st)
		}
	}
	if f.Priority, err = enumParam(r, "priority", model.Priorities); err == nil {
		if f.Escalated, err = boolParam(r, "escalated"); err == nil {
			f.Limit, err = limitParam(r)
		}
	}
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	page, err := s.svc.ListRequests(r.Context(), f, u)
	respond(s, w, r, page, err)
}

func (s *Server) createRequest(w http.ResponseWriter, r *http.Request, u model.User) {
	var in model.NewServiceRequest
	if err := decodeJSON(w, r, &in); err != nil {
		s.writeError(w, r, err)
		return
	}
	req, err := s.svc.CreateRequest(r.Context(), in, u)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, req)
}

func (s *Server) getRequest(w http.ResponseWriter, r *http.Request, u model.User) {
	req, err := s.svc.GetRequest(r.Context(), r.PathValue("requestId"), u)
	respond(s, w, r, req, err)
}

func (s *Server) updateRequest(w http.ResponseWriter, r *http.Request, u model.User) {
	var upd model.ServiceRequestUpdate
	if err := decodeJSON(w, r, &upd); err != nil {
		s.writeError(w, r, err)
		return
	}
	req, err := s.svc.UpdateRequest(r.Context(), r.PathValue("requestId"), upd, u)
	respond(s, w, r, req, err)
}

func (s *Server) getThresholds(w http.ResponseWriter, r *http.Request, _ model.User) {
	t, err := s.svc.GetThresholds(r.Context())
	respond(s, w, r, t, err)
}

func (s *Server) putThresholds(w http.ResponseWriter, r *http.Request, u model.User) {
	var in model.ThresholdRules
	if err := decodeJSON(w, r, &in); err != nil {
		s.writeError(w, r, err)
		return
	}
	t, err := s.svc.SetThresholds(r.Context(), in, u)
	respond(s, w, r, t, err)
}

func respond[T any](s *Server, w http.ResponseWriter, r *http.Request, v T, err error) {
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func enumParam[T ~string](r *http.Request, name string, allowed []T) (T, error) {
	v := T(r.URL.Query().Get(name))
	if v != "" && !model.Valid(v, allowed) {
		return "", apperr.Invalid("%s must be one of: %s", name, model.JoinEnum(allowed))
	}
	return v, nil
}

func intParam(r *http.Request, name string, fallback int) (int, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, apperr.Invalid("%s must be a whole number", name)
	}
	return n, nil
}

func limitParam(r *http.Request) (int, error) {
	n, err := intParam(r, "limit", 50)
	if err == nil && (n < 1 || n > 100) {
		err = apperr.Invalid("limit must be between 1 and 100")
	}
	return n, err
}

func timeParam(r *http.Request, name string) (time.Time, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return time.Time{}, apperr.Invalid("%s must be an ISO 8601 date-time, e.g. 2026-05-10T09:30:00Z", name)
	}
	return t, nil
}

func boolParam(r *http.Request, name string) (*bool, error) {
	switch r.URL.Query().Get(name) {
	case "":
		return nil, nil
	case "true":
		return new(true), nil
	case "false":
		return new(false), nil
	}
	return nil, apperr.Invalid("%s must be true or false", name)
}
