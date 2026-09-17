// Package metrics publishes custom CloudWatch metrics by writing Embedded Metric
// Format (EMF) lines to stdout. CloudWatch Logs turns them into metrics, so no
// CloudWatch API call is needed, which also works from Lambdas in private subnets.
package metrics

import (
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"
)

const namespace = "CampusPulse"

type Emitter struct {
	enabled bool
	service string
	mu      sync.Mutex
	out     io.Writer
}

func New(enabled bool, service string) *Emitter {
	return &Emitter{enabled: enabled, service: service, out: os.Stdout}
}

// Count records a count metric, with the service name as its only dimension.
func (e *Emitter) Count(name string, value float64) {
	if e == nil || !e.enabled || value == 0 {
		return
	}
	line, err := json.Marshal(map[string]any{
		"_aws": map[string]any{
			"Timestamp": time.Now().UnixMilli(),
			"CloudWatchMetrics": []any{map[string]any{
				"Namespace":  namespace,
				"Dimensions": [][]string{{"Service"}},
				"Metrics":    []any{map[string]string{"Name": name, "Unit": "Count"}},
			}},
		},
		"Service": e.service,
		name:      value,
	})
	if err != nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	_, _ = e.out.Write(append(line, '\n'))
}
