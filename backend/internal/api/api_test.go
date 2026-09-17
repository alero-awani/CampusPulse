package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"campuspulse/internal/api"
	"campuspulse/internal/auth"
	"campuspulse/internal/campus"
	"campuspulse/internal/ids"
	"campuspulse/internal/metrics"
	"campuspulse/internal/model"
	"campuspulse/internal/rules"
	"campuspulse/internal/service"
	"campuspulse/internal/store"
)

const deviceKey = "test-device-key"

// TestAPI runs the whole API against DynamoDB Local in freshly created tables.
// Start DynamoDB Local and set DYNAMODB_ENDPOINT to run it (make test-integration).
func TestAPI(t *testing.T) {
	endpoint := os.Getenv("DYNAMODB_ENDPOINT")
	if endpoint == "" {
		t.Skip("set DYNAMODB_ENDPOINT to run the integration test against DynamoDB Local")
	}
	ctx := context.Background()
	st, err := store.New(ctx, store.Config{Region: "us-east-1", Endpoint: endpoint, TablePrefix: ids.New("test") + "-"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateTables(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.DeleteTables(context.Background()) })

	now := time.Now()
	for _, b := range campus.Build(4) {
		for _, r := range b.Rooms {
			must(t, st.UpsertRoom(ctx, store.RoomRecord{RoomID: r.ID, Name: r.Name, Building: r.Building, Floor: r.Floor, Type: r.Type, Capacity: r.Capacity, CreatedAt: model.FormatTime(now)}))
		}
	}
	for _, u := range campus.DemoUsers {
		hash, err := auth.HashPassword("demo1234")
		must(t, err)
		must(t, st.PutUser(ctx, store.UserRecord{Email: u.Email, ID: u.ID, Name: u.Name, Role: u.Role, PasswordHash: hash}))
	}

	tokens, err := auth.NewTokens([]byte("integration-test-secret-at-least-32-bytes"), time.Hour)
	must(t, err)
	loc, _ := time.LoadLocation("Europe/Paris")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := service.New(st, auth.NewLocal(st, tokens), loc, log, metrics.New(false, "test"))
	srv := httptest.NewServer(api.New(svc, api.Config{Version: "test", DeviceKeys: []string{deviceKey}, AllowedOrigins: []string{"https://*.lovable.app"}}, log))
	defer srv.Close()
	c := &client{t: t, base: srv.URL}

	t.Run("health", func(t *testing.T) {
		c := c.on(t)
		var h model.Health
		c.expect("GET", "/health", "", nil, 200, &h)
		if h.Status != "ok" {
			t.Errorf("status = %q", h.Status)
		}
	})

	var student, student2, staff, admin string
	t.Run("login", func(t *testing.T) {
		c := c.on(t)
		c.expectError("POST", "/auth/login", "", map[string]string{"email": "staff@northbridge.edu", "password": "wrong-password"}, 401, "invalid_credentials")
		c.expectError("POST", "/auth/login", "", map[string]string{"email": "nobody@northbridge.edu", "password": "demo1234"}, 401, "invalid_credentials")
		login := func(email string) string {
			var resp model.LoginResponse
			c.expect("POST", "/auth/login", "", map[string]string{"email": email, "password": "demo1234"}, 200, &resp)
			if resp.Token == "" || resp.User.Email != strings.ToLower(strings.TrimSpace(email)) {
				t.Fatalf("bad login response %+v", resp)
			}
			return resp.Token
		}
		student, student2 = login("student@northbridge.edu"), login("Student2@NorthBridge.edu ")
		staff, admin = login("staff@northbridge.edu"), login("admin@northbridge.edu")

		var me model.User
		c.expect("GET", "/me", staff, nil, 200, &me)
		if me.Role != model.RoleStaff || me.Name != "Jordan Lee" {
			t.Errorf("GET /me = %+v", me)
		}
		c.expectError("GET", "/me", "", nil, 401, "unauthorized")
		c.expectError("GET", "/me", "not-a-token", nil, 401, "unauthorized")
	})

	room := "library-a-a203" // capacity 40
	ts := func(ago time.Duration) string { return model.FormatTime(time.Now().Add(-ago)) }

	t.Run("ingest single events", func(t *testing.T) {
		c := c.on(t)
		body := map[string]any{"event_id": "evt-overload-1", "device_id": "occ-a203", "building": "Library-A", "room_id": room,
			"event_type": "occupancy", "value": 52, "unit": "people", "severity": "normal", "timestamp": ts(2 * time.Minute)}
		c.expectError("POST", "/events", "", body, 401, "unauthorized")

		var res model.IngestResult
		c.expectDevice("POST", "/events", body, 201, &res)
		if res.Status != model.IngestStored || res.Severity != model.SeverityCritical {
			t.Errorf("first ingest = %+v, want stored and critical (client severity ignored)", res)
		}
		c.expectDevice("POST", "/events", body, 200, &res)
		if res.Status != model.IngestDuplicate {
			t.Errorf("repeat ingest = %+v, want duplicate", res)
		}

		// Appendix A format: room given by name, no event_id.
		appendixA := map[string]any{"device_id": "env-a203", "building": "Library-A", "room": "A203", "event_type": "temperature", "value": 22.4, "unit": "celsius", "timestamp": ts(10 * time.Minute)}
		c.expectDevice("POST", "/events", appendixA, 201, &res)
		if res.Status != model.IngestStored || !strings.HasPrefix(res.EventID, "evt-") {
			t.Errorf("appendix A ingest = %+v", res)
		}

		bad := map[string]any{"device_id": "occ-a203", "building": "Library-A", "room_id": room, "event_type": "occupancy", "value": "lots"}
		var e model.ErrorResponse
		c.expectDevice("POST", "/events", bad, 400, &e)
		if !strings.Contains(e.Error.Message, "must be a number") {
			t.Errorf("error = %q", e.Error.Message)
		}
	})

	t.Run("ingest batch", func(t *testing.T) {
		c := c.on(t)
		events := []map[string]any{
			{"event_id": "evt-hot-1", "device_id": "env-a203", "building": "Library-A", "room_id": room, "event_type": "temperature", "value": 38.2, "timestamp": ts(time.Minute)},
			{"event_id": "evt-hum-1", "device_id": "env-a203", "building": "Library-A", "room_id": room, "event_type": "humidity", "value": 45, "timestamp": ts(time.Minute)},
			{"event_id": "evt-energy-1", "device_id": "meter-library-a", "building": "Library-A", "event_type": "energy", "value": 12.5, "timestamp": ts(time.Minute)},
			{"event_id": "evt-leak-1", "device_id": "eq-b102", "building": "Engineering-B", "room_id": "engineering-b-b102", "event_type": "equipment", "value": "water_leak", "timestamp": ts(time.Minute)},
			{"event_id": "evt-bad-room", "device_id": "occ-x", "building": "Library-A", "room_id": "engineering-b-b102", "event_type": "occupancy", "value": 3},
			{"event_id": "evt-future", "device_id": "occ-a203", "building": "Library-A", "room_id": room, "event_type": "occupancy", "value": 3, "timestamp": model.FormatTime(time.Now().Add(time.Hour))},
		}
		var resp model.BatchResponse
		c.expectDevice("POST", "/events/batch", map[string]any{"events": events}, 200, &resp)
		if resp.Accepted != 4 || resp.Rejected != 2 {
			t.Fatalf("batch = %+v", resp)
		}
		if !strings.Contains(resp.Results[4].Error, "is not in building") || !strings.Contains(resp.Results[5].Error, "future") {
			t.Errorf("rejection messages = %q, %q", resp.Results[4].Error, resp.Results[5].Error)
		}

		tooMany := make([]map[string]any, service.MaxBatchSize+1)
		for i := range tooMany {
			tooMany[i] = events[1]
		}
		c.expectErrorDevice("POST", "/events/batch", map[string]any{"events": tooMany}, 400, "validation_error")
	})

	t.Run("rooms", func(t *testing.T) {
		c := c.on(t)
		var studentView, staffView model.List[model.Room]
		c.expect("GET", "/rooms?building=Library-A", student, nil, 200, &studentView)
		c.expect("GET", "/rooms?building=Library-A", staff, nil, 200, &staffView)
		if len(staffView.Items) != 6 {
			t.Fatalf("Library-A has %d rooms, want 6", len(staffView.Items))
		}
		a203 := find(staffView.Items, func(r model.Room) bool { return r.RoomID == room })
		if a203.Status != model.RoomOverloaded || a203.Occupancy != 52 || a203.Utilization != 1.3 || a203.Temperature == nil || *a203.Temperature != 38.2 {
			t.Errorf("A203 = %+v", a203)
		}
		if a203.OpenAlerts != 2 {
			t.Errorf("staff sees %d open alerts in A203, want 2 (occupancy, temperature)", a203.OpenAlerts)
		}
		if s := find(studentView.Items, func(r model.Room) bool { return r.RoomID == room }); s.OpenAlerts != 0 {
			t.Errorf("students should not see alert counts, got %d", s.OpenAlerts)
		}

		var overloaded model.List[model.Room]
		c.expect("GET", "/rooms?status=overloaded", staff, nil, 200, &overloaded)
		if len(overloaded.Items) != 1 {
			t.Errorf("%d overloaded rooms, want 1", len(overloaded.Items))
		}
		c.expectError("GET", "/rooms?status=crowded", staff, nil, 400, "validation_error")

		var detail model.RoomDetail
		c.expect("GET", "/rooms/"+room+"?hours=1", student, nil, 200, &detail)
		if len(detail.Readings) != 13 || len(detail.RecentEvents) < 3 {
			t.Fatalf("detail has %d readings and %d events", len(detail.Readings), len(detail.RecentEvents))
		}
		withOccupancy := 0
		for _, r := range detail.Readings {
			if r.Occupancy != nil {
				withOccupancy++
			}
		}
		if withOccupancy != 1 {
			t.Errorf("%d buckets have occupancy, want 1", withOccupancy)
		}
		c.expectError("GET", "/rooms/nowhere", staff, nil, 404, "not_found")
		c.expectError("GET", "/rooms/"+room+"?hours=48", staff, nil, 400, "validation_error")
	})

	var alertID string
	t.Run("alerts", func(t *testing.T) {
		c := c.on(t)
		c.expectError("GET", "/alerts", student, nil, 403, "forbidden")

		var open model.Page[model.Alert]
		c.expect("GET", "/alerts?status=open", staff, nil, 200, &open)
		if len(open.Items) != 3 {
			t.Fatalf("%d open alerts, want 3 (occupancy, temperature, leak)", len(open.Items))
		}

		// A second overload reading in the same room updates the existing alert.
		var res model.IngestResult
		c.expectDevice("POST", "/events", map[string]any{"device_id": "occ-a203", "building": "Library-A", "room_id": room, "event_type": "occupancy", "value": 60}, 201, &res)
		c.expect("GET", "/alerts?status=open&type=occupancy", staff, nil, 200, &open)
		if len(open.Items) != 1 || !strings.Contains(open.Items[0].Message, "Occupancy 60") {
			t.Fatalf("occupancy alerts after a repeat reading = %+v", open.Items)
		}
		alertID = open.Items[0].AlertID
		// The alert's lock item must not be reachable as an alert.
		c.expectError("PATCH", "/alerts/lock%23"+room+"%7Coccupancy", staff, map[string]string{"status": "resolved"}, 404, "not_found")

		var a model.Alert
		c.expect("PATCH", "/alerts/"+alertID, staff, map[string]string{"status": "acknowledged", "note": "Sent a steward"}, 200, &a)
		if a.Status != model.AlertAcknowledged || a.UpdatedBy == nil || *a.UpdatedBy != "Jordan Lee" {
			t.Errorf("acknowledged alert = %+v", a)
		}
		c.expect("PATCH", "/alerts/"+alertID, staff, map[string]string{"status": "resolved"}, 200, &a)
		if a.Status != model.AlertResolved {
			t.Errorf("resolved alert = %+v", a)
		}
		c.expectError("PATCH", "/alerts/"+alertID, staff, map[string]string{"status": "acknowledged"}, 409, "conflict")

		// Once resolved, the next overload opens a new alert.
		c.expectDevice("POST", "/events", map[string]any{"device_id": "occ-a203", "building": "Library-A", "room_id": room, "event_type": "occupancy", "value": 55}, 201, &res)
		c.expect("GET", "/alerts?status=open&type=occupancy", staff, nil, 200, &open)
		if len(open.Items) != 1 || open.Items[0].AlertID == alertID {
			t.Errorf("expected a new occupancy alert, got %+v", open.Items)
		}
	})

	var requestID string
	t.Run("service requests", func(t *testing.T) {
		c := c.on(t)
		newRequest := map[string]string{"title": "Student fainted", "description": "A student fainted near the entrance", "category": "medical", "building": "Library-A", "room_id": room}
		c.expectError("POST", "/service-requests", staff, newRequest, 403, "forbidden")

		var req model.ServiceRequest
		c.expect("POST", "/service-requests", student, newRequest, 201, &req)
		if req.Priority != model.PriorityUrgent || req.Status != model.RequestOpen || req.Room == nil || *req.Room != "A203" || len(req.History) != 1 {
			t.Fatalf("created request = %+v", req)
		}
		requestID = req.RequestID

		var low model.ServiceRequest
		c.expect("POST", "/service-requests", student2, map[string]string{"title": "Lost charger", "description": "Found a charger", "category": "other", "building": "StudentHub-D"}, 201, &low)
		if low.Priority != model.PriorityLow {
			t.Errorf("priority = %s, want low", low.Priority)
		}
		c.expectError("POST", "/service-requests", student, map[string]string{"title": "x", "description": "y", "category": "parking", "building": "Library-A"}, 400, "validation_error")

		c.expectError("GET", "/service-requests/"+requestID, student2, nil, 404, "not_found")
		var own model.Page[model.ServiceRequest]
		c.expect("GET", "/service-requests", student2, nil, 200, &own)
		if len(own.Items) != 1 || own.Items[0].RequestID != low.RequestID {
			t.Errorf("student2 sees %+v, want only their own request", own.Items)
		}
		var active model.Page[model.ServiceRequest]
		c.expect("GET", "/service-requests?status=open,in_progress&priority=urgent", staff, nil, 200, &active)
		if len(active.Items) != 1 || active.Items[0].History != nil {
			t.Errorf("staff urgent list = %+v", active.Items)
		}

		c.expectError("PATCH", "/service-requests/"+requestID, student, map[string]string{"status": "resolved"}, 403, "forbidden")
		var updated model.ServiceRequest
		c.expect("PATCH", "/service-requests/"+requestID, staff, map[string]any{"status": "in_progress", "assigned_to": "Campus nurse", "note": "On the way"}, 200, &updated)
		if updated.Status != model.RequestInProgress || updated.AssignedTo == nil || len(updated.History) != 4 {
			t.Errorf("updated request = %+v", updated)
		}
		c.expect("PATCH", "/service-requests/"+requestID, staff, map[string]any{"assigned_to": nil}, 200, &updated)
		if updated.AssignedTo != nil {
			t.Errorf("assigned_to = %v, want null", *updated.AssignedTo)
		}
		c.expectError("PATCH", "/service-requests/"+requestID, staff, map[string]any{}, 400, "validation_error")
	})

	t.Run("escalation", func(t *testing.T) {
		c := c.on(t)
		svc.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
		defer func() { svc.Now = time.Now }()
		n, err := svc.EscalateRequests(ctx)
		must(t, err)
		if n != 1 {
			t.Fatalf("escalated %d requests, want 1 (the overdue urgent one)", n)
		}
		var req model.ServiceRequest
		c.expect("GET", "/service-requests/"+requestID, student, nil, 200, &req)
		if !req.Escalated || req.EscalationReason == nil || req.History[len(req.History)-1].By != "System" {
			t.Errorf("request after escalation = %+v", req)
		}
	})

	t.Run("stats", func(t *testing.T) {
		c := c.on(t)
		c.expectError("GET", "/stats", student, nil, 403, "forbidden")
		var stats model.Stats
		c.expect("GET", "/stats", staff, nil, 200, &stats)
		if stats.Rooms.Total != 24 || stats.Rooms.Overloaded != 1 || stats.Alerts.Open != 3 || stats.Alerts.Critical != 3 {
			t.Errorf("rooms/alerts = %+v %+v", stats.Rooms, stats.Alerts)
		}
		if stats.ServiceRequests.Open != 2 || stats.ServiceRequests.Urgent != 1 || stats.ServiceRequests.Escalated != 1 {
			t.Errorf("requests = %+v", stats.ServiceRequests)
		}
		if stats.EnergyToday.TotalKWh != 12.5 || stats.EnergyToday.TopBuilding == nil || stats.EnergyToday.TopBuilding.Building != "Library-A" {
			t.Errorf("energy = %+v", stats.EnergyToday)
		}
		if stats.Ingestion.EventsLastHour != 8 {
			t.Errorf("events last hour = %d, want 8", stats.Ingestion.EventsLastHour)
		}

		var energy model.EnergyStats
		c.expect("GET", "/stats/energy", staff, nil, 200, &energy)
		if len(energy.Buildings) != 4 || energy.Buildings[0].Building != "Library-A" || energy.Buildings[0].Share != 1 || len(energy.Buildings[0].HourlyKWh) != 24 {
			t.Errorf("energy stats = %+v", energy)
		}
		c.expectError("GET", "/stats/energy?date=yesterday", staff, nil, 400, "validation_error")
	})

	t.Run("energy alert", func(t *testing.T) {
		c := c.on(t)
		var resp model.BatchResponse
		events := []map[string]any{
			{"device_id": "meter-engineering-b", "building": "Engineering-B", "event_type": "energy", "value": 100},
			{"device_id": "meter-engineering-b", "building": "Engineering-B", "event_type": "energy", "value": 130},
		}
		c.expectDevice("POST", "/events/batch", map[string]any{"events": events}, 200, &resp)
		var alerts model.Page[model.Alert]
		c.expect("GET", "/alerts?type=energy", staff, nil, 200, &alerts)
		if len(alerts.Items) != 1 || alerts.Items[0].Severity != model.SeverityCritical {
			t.Errorf("energy alerts = %+v", alerts.Items)
		}
	})

	t.Run("events pagination", func(t *testing.T) {
		c := c.on(t)
		seen := map[string]bool{}
		cursor := ""
		pages := 0
		for {
			var page model.Page[model.Event]
			c.expect("GET", "/events?limit=3"+cursor, staff, nil, 200, &page)
			for _, e := range page.Items {
				if seen[e.EventID] {
					t.Fatalf("event %s returned twice", e.EventID)
				}
				seen[e.EventID] = true
			}
			pages++
			if page.NextCursor == nil {
				break
			}
			cursor = "&cursor=" + *page.NextCursor
		}
		if len(seen) != 10 || pages != 4 {
			t.Errorf("saw %d events over %d pages, want 10 events over 4 pages", len(seen), pages)
		}
		c.expectError("GET", "/events?cursor=garbage", staff, nil, 400, "validation_error")
	})

	t.Run("thresholds", func(t *testing.T) {
		c := c.on(t)
		c.expectError("GET", "/thresholds", staff, nil, 403, "forbidden")
		var th model.Thresholds
		c.expect("GET", "/thresholds", admin, nil, 200, &th)
		if th.Temperature.CriticalMax != 32 {
			t.Errorf("default critical max = %v", th.Temperature.CriticalMax)
		}
		bad := rules.DefaultThresholds()
		bad.Temperature.WarningMax = 50
		c.expectError("PUT", "/thresholds", admin, bad, 400, "validation_error")

		next := rules.DefaultThresholds()
		next.Temperature.CriticalMax = 40
		c.expect("PUT", "/thresholds", admin, next, 200, &th)
		if th.Temperature.CriticalMax != 40 || th.UpdatedBy != "Alex Morgan" {
			t.Errorf("saved thresholds = %+v", th)
		}
		// 38 °C is now only a warning.
		var res model.IngestResult
		c.expectDevice("POST", "/events", map[string]any{"device_id": "env-b201", "building": "Engineering-B", "room_id": "engineering-b-b201", "event_type": "temperature", "value": 38}, 201, &res)
		if res.Severity != model.SeverityWarning {
			t.Errorf("severity with new thresholds = %s, want warning", res.Severity)
		}
	})

	t.Run("cors", func(t *testing.T) {
		req, _ := http.NewRequest("OPTIONS", srv.URL+"/stats", nil)
		req.Header.Set("Origin", "https://campuspulse.lovable.app")
		req.Header.Set("Access-Control-Request-Method", "GET")
		resp, err := http.DefaultClient.Do(req)
		must(t, err)
		resp.Body.Close()
		if resp.StatusCode != 204 || resp.Header.Get("Access-Control-Allow-Origin") != "https://campuspulse.lovable.app" {
			t.Errorf("preflight = %d, allow-origin %q", resp.StatusCode, resp.Header.Get("Access-Control-Allow-Origin"))
		}
		req.Header.Set("Origin", "https://evil.example.com")
		resp, err = http.DefaultClient.Do(req)
		must(t, err)
		resp.Body.Close()
		if resp.Header.Get("Access-Control-Allow-Origin") != "" {
			t.Error("an unknown origin was allowed")
		}
	})
}

type client struct {
	t    *testing.T
	base string
}

// on returns a client that reports failures to t, so each subtest fails on its own.
func (c *client) on(t *testing.T) *client { return &client{t: t, base: c.base} }

func (c *client) do(method, path, token string, headers map[string]string, body any) (int, []byte) {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		must(c.t, err)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, r)
	must(c.t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	must(c.t, err)
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	must(c.t, err)
	return resp.StatusCode, data
}

func (c *client) check(method, path string, status int, data []byte, want int, out any) {
	c.t.Helper()
	if status != want {
		c.t.Fatalf("%s %s = %d, want %d: %s", method, path, status, want, data)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			c.t.Fatalf("%s %s: decode %s: %v", method, path, data, err)
		}
	}
}

func (c *client) expect(method, path, token string, body any, want int, out any) {
	c.t.Helper()
	status, data := c.do(method, path, token, nil, body)
	c.check(method, path, status, data, want, out)
}

func (c *client) expectDevice(method, path string, body any, want int, out any) {
	c.t.Helper()
	status, data := c.do(method, path, "", map[string]string{"X-Device-Key": deviceKey}, body)
	c.check(method, path, status, data, want, out)
}

func (c *client) expectError(method, path, token string, body any, want int, code string) {
	c.t.Helper()
	var e model.ErrorResponse
	c.expect(method, path, token, body, want, &e)
	if e.Error.Code != code {
		c.t.Fatalf("%s %s: error code %q, want %q (%s)", method, path, e.Error.Code, code, e.Error.Message)
	}
}

func (c *client) expectErrorDevice(method, path string, body any, want int, code string) {
	c.t.Helper()
	var e model.ErrorResponse
	c.expectDevice(method, path, body, want, &e)
	if e.Error.Code != code {
		c.t.Fatalf("%s %s: error code %q, want %q", method, path, e.Error.Code, code)
	}
}

func find[T any](items []T, match func(T) bool) T {
	for _, it := range items {
		if match(it) {
			return it
		}
	}
	var zero T
	return zero
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(fmt.Errorf("unexpected error: %w", err))
	}
}
