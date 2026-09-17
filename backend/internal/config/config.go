// Package config reads settings from environment variables.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
	_ "time/tzdata" // embed time zone data so CAMPUS_TIMEZONE works in minimal containers and Lambda
)

// Version is reported by GET /health. Override at build time with
// -ldflags "-X campuspulse/internal/config.Version=1.2.3".
var Version = "0.1.0"

// LocalDeviceKey is the device key used when DEVICE_API_KEYS is not set locally.
const LocalDeviceKey = "local-device-key"

type Config struct {
	Env                string // "local" or "aws"
	Port               string
	Region             string
	DynamoDBEndpoint   string // set for DynamoDB Local; empty means real AWS
	TablePrefix        string
	AuthMode           string // "local" (password hashes in DynamoDB) or "cognito"
	CognitoClientID    string
	JWTSecret          []byte
	TokenTTL           time.Duration
	DeviceKeys         []string
	AllowedOrigins     []string
	Location           *time.Location // campus time zone, used for "today" and hourly energy
	EscalationInterval time.Duration  // how often the local server checks requests for escalation; 0 disables it
	LogLevel           slog.Level
	LogFormat          string // "text" or "json"
	EMF                bool   // write CloudWatch Embedded Metric Format lines
}

// Load reads the configuration. Outside local mode, secrets must be provided
// explicitly. In local mode, missing secrets get development defaults, and a
// warning is returned for each one.
func Load() (Config, []string, error) {
	var warnings []string
	c := Config{
		Env:              env("APP_ENV", "local"),
		Port:             env("PORT", "8080"),
		Region:           env("AWS_REGION", "us-east-1"),
		DynamoDBEndpoint: os.Getenv("DYNAMODB_ENDPOINT"),
		TablePrefix:      env("TABLE_PREFIX", "campuspulse-"),
	}
	local := c.Env == "local"

	var err error
	if c.TokenTTL, err = time.ParseDuration(env("TOKEN_TTL", "1h")); err != nil {
		return c, nil, fmt.Errorf("TOKEN_TTL: %w", err)
	}
	if c.EscalationInterval, err = time.ParseDuration(env("ESCALATION_INTERVAL", "1m")); err != nil {
		return c, nil, fmt.Errorf("ESCALATION_INTERVAL: %w", err)
	}

	c.AuthMode = env("AUTH_MODE", "local")
	switch c.AuthMode {
	case "local":
	case "cognito":
		if c.CognitoClientID = os.Getenv("COGNITO_CLIENT_ID"); c.CognitoClientID == "" {
			return c, nil, errors.New("COGNITO_CLIENT_ID is required when AUTH_MODE is cognito")
		}
	default:
		return c, nil, errors.New("AUTH_MODE must be local or cognito")
	}

	switch secret := os.Getenv("JWT_SECRET"); {
	case c.AuthMode == "cognito":
		// Cognito signs the tokens; no local secret is needed.
	case len(secret) >= 32:
		c.JWTSecret = []byte(secret)
	case secret != "":
		return c, nil, errors.New("JWT_SECRET must be at least 32 characters")
	case local:
		b := make([]byte, 32)
		_, _ = rand.Read(b)
		c.JWTSecret = []byte(hex.EncodeToString(b))
		warnings = append(warnings, "JWT_SECRET is not set: using a random secret, so sessions end when the server restarts")
	default:
		return c, nil, errors.New("JWT_SECRET is required")
	}

	c.DeviceKeys = splitList(os.Getenv("DEVICE_API_KEYS"))
	switch {
	case len(c.DeviceKeys) > 0:
	case local:
		c.DeviceKeys = []string{LocalDeviceKey}
		warnings = append(warnings, "DEVICE_API_KEYS is not set: sensors must send the development key "+LocalDeviceKey)
	default:
		// Fail closed: with no keys configured, every sensor request is rejected.
		// Functions that don't receive sensor data (such as login) need no keys.
		warnings = append(warnings, "DEVICE_API_KEYS is not set: sensor endpoints will reject all requests")
	}

	c.AllowedOrigins = splitList(os.Getenv("CORS_ALLOWED_ORIGINS"))
	if len(c.AllowedOrigins) == 0 && local {
		c.AllowedOrigins = []string{"http://localhost:5173", "http://localhost:8081", "https://*.lovable.app", "https://*.lovableproject.com"}
	}

	if c.Location, err = time.LoadLocation(env("CAMPUS_TIMEZONE", "Europe/Paris")); err != nil {
		return c, nil, fmt.Errorf("CAMPUS_TIMEZONE: %w", err)
	}
	if err := c.LogLevel.UnmarshalText([]byte(env("LOG_LEVEL", "info"))); err != nil {
		return c, nil, fmt.Errorf("LOG_LEVEL: %w", err)
	}
	c.LogFormat = env("LOG_FORMAT", map[bool]string{true: "text", false: "json"}[local])

	inLambda := os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != ""
	c.EMF = env("METRICS_EMF", map[bool]string{true: "true", false: "false"}[inLambda]) == "true"
	return c, warnings, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func splitList(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
