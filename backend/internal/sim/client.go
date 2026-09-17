package sim

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"campuspulse/internal/model"
)

// Client sends simulated data to the CampusPulse API.
type Client struct {
	base      string
	deviceKey string
	http      *http.Client
}

func NewClient(baseURL, deviceKey string) *Client {
	return &Client{base: strings.TrimRight(baseURL, "/"), deviceKey: deviceKey, http: &http.Client{Timeout: 30 * time.Second}}
}

// errRetryable marks failures worth retrying: network errors, throttling, and server errors.
var errRetryable = errors.New("retryable")

// SendBatch posts events to /events/batch, retrying with backoff. Retries are
// safe because every event has a unique event_id and duplicates are ignored.
func (c *Client) SendBatch(ctx context.Context, events []model.EventInput) (model.BatchResponse, time.Duration, error) {
	var (
		resp    model.BatchResponse
		elapsed time.Duration
		err     error
	)
	for attempt := range 4 {
		if attempt > 0 {
			backoff := time.Duration(250*(1<<attempt))*time.Millisecond + time.Duration(rand.IntN(200))*time.Millisecond
			select {
			case <-ctx.Done():
				return resp, elapsed, ctx.Err()
			case <-time.After(backoff):
			}
		}
		start := time.Now()
		err = c.do(ctx, http.MethodPost, "/events/batch", map[string]string{"X-Device-Key": c.deviceKey}, model.BatchRequest{Events: events}, &resp)
		elapsed = time.Since(start)
		if err == nil || !errors.Is(err, errRetryable) {
			return resp, elapsed, err
		}
	}
	return resp, elapsed, err
}

func (c *Client) Login(ctx context.Context, email, password string) (string, error) {
	var resp model.LoginResponse
	err := c.do(ctx, http.MethodPost, "/auth/login", nil, model.LoginRequest{Email: email, Password: password}, &resp)
	return resp.Token, err
}

func (c *Client) CreateRequest(ctx context.Context, token string, req model.NewServiceRequest) (model.ServiceRequest, error) {
	var resp model.ServiceRequest
	err := c.do(ctx, http.MethodPost, "/service-requests", map[string]string{"Authorization": "Bearer " + token}, req, &resp)
	return resp, err
}

// APIError is an error response from the API.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Message) }

func (c *Client) do(ctx context.Context, method, path string, headers map[string]string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: %v", errRetryable, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%w: %v", errRetryable, err)
	}
	if resp.StatusCode >= 300 {
		apiErr := &APIError{Status: resp.StatusCode}
		var e model.ErrorResponse
		if json.Unmarshal(data, &e) == nil {
			apiErr.Code, apiErr.Message = e.Error.Code, e.Error.Message
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return fmt.Errorf("%w: %v", errRetryable, apiErr)
		}
		return apiErr
	}
	return json.Unmarshal(data, out)
}
