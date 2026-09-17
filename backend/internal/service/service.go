// Package service implements the API's behavior on top of the store: validating
// input, applying the processing rules, and enforcing who may see what.
package service

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"campuspulse/internal/apperr"
	"campuspulse/internal/auth"
	"campuspulse/internal/metrics"
	"campuspulse/internal/model"
	"campuspulse/internal/rules"
	"campuspulse/internal/store"
)

// cacheTTL is how long room layouts and thresholds are cached in memory. With
// several Lambda instances, a threshold change can take this long to apply everywhere.
const cacheTTL = 30 * time.Second

type Service struct {
	store   *store.Store
	auth    auth.Authenticator
	loc     *time.Location
	log     *slog.Logger
	metrics *metrics.Emitter
	// Now is replaceable in tests.
	Now func() time.Time

	mu                sync.Mutex
	campus            *campusIndex
	campusExpires     time.Time
	thresholds        *model.Thresholds
	thresholdsExpires time.Time
}

func New(st *store.Store, authn auth.Authenticator, loc *time.Location, log *slog.Logger, m *metrics.Emitter) *Service {
	return &Service{store: st, auth: authn, loc: loc, log: log, metrics: m, Now: time.Now}
}

// campusIndex is the room layout (not the live readings), used to validate input.
type campusIndex struct {
	rooms     map[string]store.RoomRecord
	byName    map[string]string // "building|room name" (lowercase) -> room ID
	buildings []string
}

func nameKey(building, room string) string {
	return strings.ToLower(strings.TrimSpace(building) + "|" + strings.TrimSpace(room))
}

func (c *campusIndex) hasBuilding(b string) bool {
	i := sort.SearchStrings(c.buildings, b)
	return i < len(c.buildings) && c.buildings[i] == b
}

func (s *Service) campusLayout(ctx context.Context) (*campusIndex, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.campus != nil && s.Now().Before(s.campusExpires) {
		return s.campus, nil
	}
	rooms, err := s.store.ListRooms(ctx)
	if err != nil {
		return nil, err
	}
	idx := &campusIndex{rooms: map[string]store.RoomRecord{}, byName: map[string]string{}}
	seen := map[string]bool{}
	for _, r := range rooms {
		idx.rooms[r.RoomID] = r
		idx.byName[nameKey(r.Building, r.Name)] = r.RoomID
		if !seen[r.Building] {
			seen[r.Building] = true
			idx.buildings = append(idx.buildings, r.Building)
		}
	}
	sort.Strings(idx.buildings)
	s.campus, s.campusExpires = idx, s.Now().Add(cacheTTL)
	return idx, nil
}

func (s *Service) currentThresholds(ctx context.Context) (model.Thresholds, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.thresholds != nil && s.Now().Before(s.thresholdsExpires) {
		return *s.thresholds, nil
	}
	t, err := s.store.GetThresholds(ctx)
	if err != nil {
		return model.Thresholds{}, err
	}
	if t == nil {
		t = &model.Thresholds{ThresholdRules: rules.DefaultThresholds(), UpdatedAt: model.FormatTime(s.Now()), UpdatedBy: "System"}
	}
	s.thresholds, s.thresholdsExpires = t, s.Now().Add(cacheTTL)
	return *t, nil
}

func (s *Service) validBuilding(ctx context.Context, building string) error {
	if building == "" {
		return nil
	}
	c, err := s.campusLayout(ctx)
	if err != nil {
		return err
	}
	if !c.hasBuilding(building) {
		return apperr.Invalid("unknown building %q", building)
	}
	return nil
}

func isStaff(u model.User) bool { return u.Role == model.RoleStaff || u.Role == model.RoleAdmin }

func conflictOr(err error, message string) error {
	if errors.Is(err, store.ErrConflict) {
		return apperr.Conflict(message)
	}
	return err
}

func round(v float64, decimals int) float64 {
	p := math.Pow(10, float64(decimals))
	return math.Round(v*p) / p
}

func roundPtr(v *float64, decimals int) *float64 {
	if v == nil {
		return nil
	}
	r := round(*v, decimals)
	return &r
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
