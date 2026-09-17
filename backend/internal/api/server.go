// Package api exposes the service over HTTP using the standard library router.
package api

import (
	"crypto/sha256"
	"log/slog"
	"net/http"
	"time"

	"campuspulse/internal/apperr"
	"campuspulse/internal/model"
	"campuspulse/internal/service"
)

type Config struct {
	Version        string
	DeviceKeys     []string
	AllowedOrigins []string
}

type Server struct {
	svc          *service.Service
	log          *slog.Logger
	version      string
	deviceKeys   [][sha256.Size]byte
	origins      []string
	loginLimiter *limiter
}

// New returns the API's HTTP handler with all middleware applied.
func New(svc *service.Service, cfg Config, log *slog.Logger) http.Handler {
	s := &Server{
		svc:     svc,
		log:     log,
		version: cfg.Version,
		origins: cfg.AllowedOrigins,
		// 10 login attempts per client and email every 5 minutes slows password guessing.
		loginLimiter: newLimiter(10, 5*time.Minute),
	}
	for _, k := range cfg.DeviceKeys {
		s.deviceKeys = append(s.deviceKeys, sha256.Sum256([]byte(k)))
	}

	anyone := []model.Role{model.RoleStudent, model.RoleStaff, model.RoleAdmin}
	staff := []model.Role{model.RoleStaff, model.RoleAdmin}
	admin := []model.Role{model.RoleAdmin}
	students := []model.Role{model.RoleStudent}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("POST /auth/login", s.login)

	mux.Handle("POST /events", s.device(s.postEvent))
	mux.Handle("POST /events/batch", s.device(s.postEventBatch))

	mux.Handle("GET /me", s.user(anyone, s.me))
	mux.Handle("GET /stats", s.user(staff, s.stats))
	mux.Handle("GET /stats/energy", s.user(staff, s.energyStats))
	mux.Handle("GET /rooms", s.user(anyone, s.listRooms))
	mux.Handle("GET /rooms/{roomId}", s.user(anyone, s.getRoom))
	mux.Handle("GET /events", s.user(staff, s.listEvents))
	mux.Handle("GET /alerts", s.user(staff, s.listAlerts))
	mux.Handle("PATCH /alerts/{alertId}", s.user(staff, s.updateAlert))
	mux.Handle("GET /service-requests", s.user(anyone, s.listRequests))
	mux.Handle("POST /service-requests", s.user(students, s.createRequest))
	mux.Handle("GET /service-requests/{requestId}", s.user(anyone, s.getRequest))
	mux.Handle("PATCH /service-requests/{requestId}", s.user(staff, s.updateRequest))
	mux.Handle("GET /thresholds", s.user(admin, s.getThresholds))
	mux.Handle("PUT /thresholds", s.user(admin, s.putThresholds))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.writeError(w, r, apperr.NotFound("route "+r.Method+" "+r.URL.Path))
	})

	return s.logRequests(s.recoverPanics(s.cors(securityHeaders(mux))))
}
