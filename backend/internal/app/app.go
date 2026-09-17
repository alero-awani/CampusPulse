// Package app wires configuration, storage, and the API together. The local
// server and the Lambda functions share it.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"

	"campuspulse/internal/api"
	"campuspulse/internal/auth"
	"campuspulse/internal/config"
	"campuspulse/internal/metrics"
	"campuspulse/internal/service"
	"campuspulse/internal/store"
)

type App struct {
	Config  config.Config
	Log     *slog.Logger
	Store   *store.Store
	Service *service.Service
	Handler http.Handler
}

// Build creates the application. component names the process in logs and metrics.
func Build(ctx context.Context, component string) (*App, error) {
	cfg, warnings, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("configuration: %w", err)
	}
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	var handler slog.Handler = slog.NewJSONHandler(os.Stdout, opts)
	if cfg.LogFormat == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	log := slog.New(handler).With("component", component)
	for _, w := range warnings {
		log.Warn(w)
	}

	st, err := store.New(ctx, store.Config{Region: cfg.Region, Endpoint: cfg.DynamoDBEndpoint, TablePrefix: cfg.TablePrefix})
	if err != nil {
		return nil, err
	}
	var authn auth.Authenticator
	if cfg.AuthMode == "cognito" {
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
		if err != nil {
			return nil, err
		}
		authn = auth.NewCognito(awsCfg, cfg.CognitoClientID)
	} else {
		tokens, err := auth.NewTokens(cfg.JWTSecret, cfg.TokenTTL)
		if err != nil {
			return nil, err
		}
		authn = auth.NewLocal(st, tokens)
	}
	svc := service.New(st, authn, cfg.Location, log, metrics.New(cfg.EMF, component))
	h := api.New(svc, api.Config{Version: config.Version, DeviceKeys: cfg.DeviceKeys, AllowedOrigins: cfg.AllowedOrigins}, log)
	return &App{Config: cfg, Log: log, Store: st, Service: svc, Handler: h}, nil
}
