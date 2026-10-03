// Command setup creates the DynamoDB tables and loads the campus layout and the
// default alert rules. It is safe to run more than once. Accounts are not stored in
// DynamoDB: in AWS they live in Cognito, and local mode has the demo accounts built in.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"campuspulse/internal/campus"
	"campuspulse/internal/config"
	"campuspulse/internal/model"
	"campuspulse/internal/rules"
	"campuspulse/internal/store"
)

func main() {
	reset := flag.Bool("reset", false, "delete all tables first (erases all data)")
	buildings := flag.Int("buildings", 4, "number of buildings (more than 4 adds generated Annex buildings)")
	flag.Parse()

	if err := run(*reset, *buildings); err != nil {
		fmt.Fprintln(os.Stderr, "setup failed:", err)
		os.Exit(1)
	}
}

func run(reset bool, buildings int) error {
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

	return nil
}
