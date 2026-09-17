// Command simulator generates campus sensor events and sends them to the API.
//
//	simulator live      stream readings in real time, optionally with student requests
//	simulator backfill  load past hours of readings so charts have history
//	simulator load      send batches as fast as possible and report latency
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"
	"time"

	"campuspulse/internal/config"
	"campuspulse/internal/model"
	"campuspulse/internal/sim"
)

const usage = `Usage: simulator <live|backfill|load> [flags]

  live      stream readings in real time (Ctrl+C to stop)
  backfill  load past hours of readings so charts have history
  load      send batches as fast as possible and report latency

Run "simulator <command> -h" for the flags of a command.
`

type common struct {
	api         string
	deviceKey   string
	buildings   int
	anomalyRate float64
	anomalyLog  string
	seed        uint64
	timezone    string
	batch       int
	concurrency int
}

func (c *common) register(fs *flag.FlagSet, defaultRate float64) {
	fs.StringVar(&c.api, "api", env("CAMPUSPULSE_API", "http://localhost:8080"), "API base URL (env CAMPUSPULSE_API)")
	fs.StringVar(&c.deviceKey, "device-key", env("DEVICE_API_KEY", config.LocalDeviceKey), "device API key (env DEVICE_API_KEY)")
	fs.IntVar(&c.buildings, "buildings", 4, "number of buildings; must match what setup created")
	fs.Float64Var(&c.anomalyRate, "anomaly-rate", defaultRate, "chance per room per tick that a problem starts")
	fs.StringVar(&c.anomalyLog, "anomaly-log", "", "append injected problems to this JSON Lines file")
	fs.Uint64Var(&c.seed, "seed", 0, "random seed for repeatable runs (0 = random)")
	fs.StringVar(&c.timezone, "timezone", env("CAMPUS_TIMEZONE", "Europe/Paris"), "campus time zone for daily patterns")
	fs.IntVar(&c.batch, "batch", 25, "events per request (max 25)")
	fs.IntVar(&c.concurrency, "concurrency", 4, "requests sent in parallel")
}

func (c *common) setup() (*sim.Simulator, *sim.Client, func(), error) {
	loc, err := time.LoadLocation(c.timezone)
	if err != nil {
		return nil, nil, nil, err
	}
	if c.batch < 1 || c.batch > 25 {
		return nil, nil, nil, errors.New("-batch must be between 1 and 25")
	}
	var logFile io.Writer
	closeLog := func() {}
	if c.anomalyLog != "" {
		f, err := os.OpenFile(c.anomalyLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, nil, nil, err
		}
		logFile, closeLog = f, func() { f.Close() }
	}
	s := sim.New(sim.Options{Buildings: c.buildings, AnomalyRate: c.anomalyRate, Seed: c.seed, Location: loc, AnomalyLog: logFile})
	return s, sim.NewClient(c.api, c.deviceKey), closeLog, nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch os.Args[1] {
	case "live":
		err = live(ctx, os.Args[2:])
	case "backfill":
		err = backfill(ctx, os.Args[2:])
	case "load":
		err = load(ctx, os.Args[2:])
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func live(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("live", flag.ExitOnError)
	var c common
	c.register(fs, 0.01)
	interval := fs.Duration("interval", 30*time.Second, "time between readings")
	duration := fs.Duration("duration", 0, "stop after this long (0 = run until Ctrl+C)")
	studentEmail := fs.String("student-email", "", "also submit service requests as this student")
	studentPassword := fs.String("student-password", "", "password for -student-email")
	requestEvery := fs.Duration("request-every", 3*time.Minute, "average time between service requests")
	_ = fs.Parse(args)

	s, client, closeLog, err := c.setup()
	if err != nil {
		return err
	}
	defer closeLog()
	if *duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *duration)
		defer cancel()
	}

	fmt.Printf("Streaming readings from %d rooms to %s every %s (Ctrl+C to stop)\n", len(s.Rooms()), c.api, *interval)
	var total result
	tick := func(now time.Time) {
		anomaliesBefore := s.Anomalies
		events := s.Tick(now, *interval)
		r := send(ctx, client, events, c.batch, c.concurrency)
		total.add(r)
		fmt.Printf("%s  %s  new problems %d\n", now.Format("15:04:05"), r.summary(), s.Anomalies-anomaliesBefore)
		r.printErrors()
	}

	var token string
	nextRequest := time.Now().Add(*requestEvery / 2)
	submitRequest := func() {
		if *studentEmail == "" || time.Now().Before(nextRequest) {
			return
		}
		nextRequest = time.Now().Add(time.Duration(float64(*requestEvery) * (0.5 + rand.Float64())))
		if token == "" {
			t, err := client.Login(ctx, *studentEmail, *studentPassword)
			if err != nil {
				fmt.Println("  student login failed:", err)
				return
			}
			token = t
		}
		req, err := client.CreateRequest(ctx, token, s.RandomRequest())
		var apiErr *sim.APIError
		if errors.As(err, &apiErr) && apiErr.Status == 401 {
			token = "" // session expired; log in again next time
		}
		if err != nil {
			fmt.Println("  service request failed:", err)
			return
		}
		fmt.Printf("  service request: %q in %s (priority %s)\n", req.Title, *req.Room, req.Priority)
	}

	tick(time.Now())
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			fmt.Printf("\nStopped. Total: %s, problems injected %d\n", total.summary(), s.Anomalies)
			return nil
		case now := <-ticker.C:
			tick(now)
			submitRequest()
		}
	}
}

func backfill(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("backfill", flag.ExitOnError)
	var c common
	c.register(fs, 0.004)
	hours := fs.Int("hours", 24, "how many past hours to generate")
	step := fs.Duration("step", 15*time.Minute, "time between generated readings")
	_ = fs.Parse(args)

	s, client, closeLog, err := c.setup()
	if err != nil {
		return err
	}
	defer closeLog()

	end := time.Now()
	start := end.Add(-time.Duration(*hours) * time.Hour).Truncate(*step)
	ticks := int(end.Sub(start) / *step)
	fmt.Printf("Backfilling %d hours (%d steps of %s) to %s\n", *hours, ticks, *step, c.api)

	var total result
	began := time.Now()
	for i, t := 0, start; t.Before(end); i, t = i+1, t.Add(*step) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r := send(ctx, client, s.Tick(t, *step), c.batch, c.concurrency)
		total.add(r)
		r.printErrors()
		if (i+1)%12 == 0 || !t.Add(*step).Before(end) {
			fmt.Printf("  up to %s  %s\n", t.In(time.Local).Format("Mon 15:04"), total.summary())
		}
	}
	fmt.Printf("Done in %s. %s, problems injected %d\n", time.Since(began).Round(time.Second), total.summary(), s.Anomalies)
	return nil
}

func load(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("load", flag.ExitOnError)
	var c common
	c.register(fs, 0)
	requests := fs.Int("requests", 200, "number of batch requests to send")
	_ = fs.Parse(args)
	if c.concurrency < 1 {
		return errors.New("-concurrency must be at least 1")
	}

	s, client, closeLog, err := c.setup()
	if err != nil {
		return err
	}
	defer closeLog()

	var events []model.EventInput
	for now := time.Now(); len(events) < *requests*c.batch; now = now.Add(time.Millisecond) {
		events = append(events, s.Tick(now, time.Minute)...)
	}
	fmt.Printf("Sending %d requests of %d events with %d in parallel to %s\n", *requests, c.batch, c.concurrency, c.api)

	began := time.Now()
	r := send(ctx, client, events[:*requests*c.batch], c.batch, c.concurrency)
	elapsed := time.Since(began)
	r.printErrors()

	slices.Sort(r.latencies)
	pct := func(p float64) time.Duration {
		if len(r.latencies) == 0 {
			return 0
		}
		return r.latencies[int(math.Ceil(p*float64(len(r.latencies))))-1].Round(time.Millisecond)
	}
	fmt.Printf("\n%s\n", r.summary())
	fmt.Printf("Duration     %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("Throughput   %.0f events/s, %.1f requests/s\n", float64(r.stored+r.duplicates)/elapsed.Seconds(), float64(len(r.latencies))/elapsed.Seconds())
	fmt.Printf("Latency      p50 %s  p95 %s  p99 %s  max %s\n", pct(0.50), pct(0.95), pct(0.99), pct(1))
	return nil
}

// result tallies the outcome of sending events.
type result struct {
	stored, duplicates, rejected, failed, requestErrors int
	latencies                                           []time.Duration
	errors                                              []string
}

func (r *result) add(o result) {
	r.stored += o.stored
	r.duplicates += o.duplicates
	r.rejected += o.rejected
	r.failed += o.failed
	r.requestErrors += o.requestErrors
	r.latencies = append(r.latencies, o.latencies...)
}

func (r *result) summary() string {
	s := fmt.Sprintf("stored %d  duplicate %d  rejected %d  failed %d", r.stored, r.duplicates, r.rejected, r.failed)
	if r.requestErrors > 0 {
		s += fmt.Sprintf("  request errors %d", r.requestErrors)
	}
	return s
}

func (r *result) printErrors() {
	for _, e := range r.errors {
		fmt.Println("  !", e)
	}
}

// send posts events in batches, several at a time.
func send(ctx context.Context, client *sim.Client, events []model.EventInput, batchSize, concurrency int) result {
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out result
		sem = make(chan struct{}, concurrency)
	)
	for start := 0; start < len(events); start += batchSize {
		batch := events[start:min(start+batchSize, len(events))]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			resp, elapsed, err := client.SendBatch(ctx, batch)
			mu.Lock()
			defer mu.Unlock()
			out.latencies = append(out.latencies, elapsed)
			if err != nil {
				out.requestErrors++
				if len(out.errors) < 5 && !errors.Is(err, context.Canceled) {
					out.errors = append(out.errors, err.Error())
				}
				return
			}
			out.stored += resp.Accepted
			out.duplicates += resp.Duplicates
			out.rejected += resp.Rejected
			out.failed += resp.Failed
			for _, res := range resp.Results {
				if res.Status == model.IngestRejected && len(out.errors) < 5 {
					out.errors = append(out.errors, "rejected "+res.EventID+": "+res.Error)
				}
			}
		}()
	}
	wg.Wait()
	return out
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
