// Command api runs the CampusPulse API as a regular HTTP server, for local
// development or a container.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"campuspulse/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, err := app.Build(ctx, "api")
	if err != nil {
		fmt.Fprintln(os.Stderr, "startup failed:", err)
		os.Exit(1)
	}
	if err := a.Store.Ping(ctx); err != nil {
		a.Log.Error("cannot reach the database or its tables are missing; start DynamoDB Local and run `make setup`", "error", err)
	}

	// In AWS a scheduled Lambda does this; locally the server does it itself.
	if a.Config.EscalationInterval > 0 {
		go func() {
			ticker := time.NewTicker(a.Config.EscalationInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if _, err := a.Service.EscalateRequests(ctx); err != nil {
						a.Log.Warn("escalation check failed", "error", err)
					}
				}
			}
		}()
	}

	srv := &http.Server{
		Addr:              ":" + a.Config.Port,
		Handler:           a.Handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	a.Log.Info("CampusPulse API listening", "url", "http://localhost:"+a.Config.Port, "env", a.Config.Env)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		a.Log.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
