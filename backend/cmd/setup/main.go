// Command setup creates the DynamoDB tables and loads the campus layout, default
// alert rules, and (locally) demo accounts. It is safe to run more than once.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"campuspulse/internal/auth"
	"campuspulse/internal/campus"
	"campuspulse/internal/config"
	"campuspulse/internal/model"
	"campuspulse/internal/rules"
	"campuspulse/internal/store"
)

func main() {
	reset := flag.Bool("reset", false, "delete all tables first (erases all data)")
	buildings := flag.Int("buildings", 4, "number of buildings (more than 4 adds generated Annex buildings)")
	password := flag.String("demo-password", "demo1234", "password for the demo accounts")
	allowDemo := flag.Bool("allow-demo-users", false, "create demo accounts even when APP_ENV is not local")
	flag.Parse()

	if err := run(*reset, *buildings, *password, *allowDemo); err != nil {
		fmt.Fprintln(os.Stderr, "setup failed:", err)
		os.Exit(1)
	}
}

func run(reset bool, buildings int, password string, allowDemo bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cfg, _, err := config.Load()
	if err != nil {
		return err
	}
	st, err := store.New(ctx, store.Config{Region: cfg.Region, Endpoint: cfg.DynamoDBEndpoint, TablePrefix: cfg.TablePrefix})
	if err != nil {
		return err
	}
	target := cfg.DynamoDBEndpoint
	if target == "" {
		target = "AWS region " + cfg.Region
	}
	fmt.Printf("Database: %s (table prefix %q)\n", target, cfg.TablePrefix)

	if reset {
		fmt.Println("Deleting tables...")
		if err := st.DeleteTables(ctx); err != nil {
			return err
		}
	}
	fmt.Println("Creating tables...")
	if err := st.CreateTables(ctx); err != nil {
		return err
	}

	now := model.FormatTime(time.Now())
	rooms := 0
	layout := campus.Build(buildings)
	for _, b := range layout {
		for _, r := range b.Rooms {
			if err := st.UpsertRoom(ctx, store.RoomRecord{
				RoomID: r.ID, Name: r.Name, Building: r.Building, Floor: r.Floor, Type: r.Type, Capacity: r.Capacity, CreatedAt: now,
			}); err != nil {
				return fmt.Errorf("save room %s: %w", r.ID, err)
			}
			rooms++
		}
	}
	fmt.Printf("Saved %d buildings and %d rooms\n", len(layout), rooms)

	existing, err := st.GetThresholds(ctx)
	if err != nil {
		return err
	}
	if existing == nil || reset {
		if err := st.PutThresholds(ctx, model.Thresholds{ThresholdRules: rules.DefaultThresholds(), UpdatedAt: now, UpdatedBy: "System"}); err != nil {
			return err
		}
		fmt.Println("Saved default alert rules")
	}

	if cfg.Env != "local" && !allowDemo {
		fmt.Println("Skipped demo accounts (APP_ENV is not local; pass -allow-demo-users to create them)")
		return nil
	}
	for _, u := range campus.DemoUsers {
		hash, err := auth.HashPassword(password)
		if err != nil {
			return err
		}
		if err := st.PutUser(ctx, store.UserRecord{Email: u.Email, ID: u.ID, Name: u.Name, Role: u.Role, PasswordHash: hash}); err != nil {
			return err
		}
	}
	fmt.Printf("\nDemo accounts (password %q):\n", password)
	for _, u := range campus.DemoUsers {
		fmt.Printf("  %-26s %-12s %s\n", u.Email, u.Name, strings.ToUpper(string(u.Role)))
	}
	return nil
}
