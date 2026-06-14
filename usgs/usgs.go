// Package usgs is the library behind the usgs command line:
// the HTTP client, request shaping, and typed data models for the USGS
// FDSNWS Earthquake Event Web Service (earthquake.usgs.gov/fdsnws/event/1).
//
// The USGS API is free and requires no API key. It returns seismic events
// worldwide in GeoJSON format with magnitude, location, depth, and metadata.
package usgs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// Host is the API host this client talks to.
const Host = "earthquake.usgs.gov"

// baseEndpoint is the root of the FDSNWS event service.
const baseEndpoint = "https://earthquake.usgs.gov/fdsnws/event/1"

// Config holds all tunable parameters for the Client.
type Config struct {
	BaseURL   string
	UserAgent string
	Rate      time.Duration
	Timeout   time.Duration
	Retries   int
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		BaseURL:   baseEndpoint,
		UserAgent: "usgs-cli/0.1 (tamnd87@gmail.com)",
		Rate:      300 * time.Millisecond,
		Timeout:   15 * time.Second,
		Retries:   3,
	}
}

// Client talks to the USGS earthquake API over HTTP.
type Client struct {
	cfg  Config
	http *http.Client
	mu   sync.Mutex
	last time.Time
}

// NewClient returns a Client configured with cfg.
func NewClient(cfg Config) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: cfg.Timeout},
	}
}

// QueryParams holds optional filters for a query.
type QueryParams struct {
	Start   string
	End     string
	MinMag  string
	MaxMag  string
	Limit   int
	OrderBy string
}

// Query calls /fdsnws/event/1/query and returns a slice of Earthquake records.
func (c *Client) Query(ctx context.Context, p QueryParams) ([]Earthquake, error) {
	q := url.Values{}
	q.Set("format", "geojson")
	if p.Start != "" {
		q.Set("starttime", p.Start)
	}
	if p.End != "" {
		q.Set("endtime", p.End)
	}
	if p.MinMag != "" {
		q.Set("minmagnitude", p.MinMag)
	}
	if p.MaxMag != "" {
		q.Set("maxmagnitude", p.MaxMag)
	}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.OrderBy != "" {
		q.Set("orderby", p.OrderBy)
	}
	body, err := c.get(ctx, c.cfg.BaseURL+"/query?"+q.Encode())
	if err != nil {
		return nil, err
	}
	return parseFeatureCollection(body)
}

// Recent returns significant earthquakes from the last 30 days (mag >= 6).
func (c *Client) Recent(ctx context.Context) ([]Earthquake, error) {
	start := time.Now().UTC().AddDate(0, 0, -30).Format("2006-01-02")
	return c.Query(ctx, QueryParams{
		Start:   start,
		MinMag:  "6.0",
		Limit:   50,
		OrderBy: "time",
	})
}

func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.cfg.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) ([]byte, bool, error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	return b, err != nil, err
}

func (c *Client) pace() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cfg.Rate <= 0 {
		return
	}
	if wait := c.cfg.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// Earthquake is one seismic event from the USGS catalog, with all numeric
// fields pre-formatted as strings for clean JSON/tabular output.
type Earthquake struct {
	Place     string `kit:"id" json:"place"`
	Magnitude string `json:"magnitude"` // "%.1f"
	Time      string `json:"time"`      // RFC3339 UTC
	Depth     string `json:"depth_km"`  // "%.1f"
	Latitude  string `json:"latitude"`  // "%.4f"
	Longitude string `json:"longitude"` // "%.4f"
	Type      string `json:"type"`
}

// --- raw API response types ---

type rawFeatureCollection struct {
	Type     string       `json:"type"`
	Features []rawFeature `json:"features"`
}

type rawFeature struct {
	Type       string        `json:"type"`
	ID         string        `json:"id"`
	Properties rawProperties `json:"properties"`
	Geometry   rawGeometry   `json:"geometry"`
}

type rawProperties struct {
	Mag   float64 `json:"mag"`
	Place string  `json:"place"`
	Time  int64   `json:"time"`
	Type  string  `json:"type"`
}

type rawGeometry struct {
	Type        string    `json:"type"`
	Coordinates []float64 `json:"coordinates"`
}

func parseFeatureCollection(body []byte) ([]Earthquake, error) {
	var fc rawFeatureCollection
	if err := json.Unmarshal(body, &fc); err != nil {
		return nil, fmt.Errorf("decode feature collection: %w", err)
	}
	out := make([]Earthquake, 0, len(fc.Features))
	for _, f := range fc.Features {
		out = append(out, convertFeature(f))
	}
	return out, nil
}

func convertFeature(f rawFeature) Earthquake {
	eq := Earthquake{
		Place:     f.Properties.Place,
		Magnitude: fmt.Sprintf("%.1f", f.Properties.Mag),
		Time:      time.Unix(f.Properties.Time/1000, 0).UTC().Format(time.RFC3339),
		Type:      f.Properties.Type,
		Depth:     "0.0",
		Latitude:  "0.0000",
		Longitude: "0.0000",
	}
	if len(f.Geometry.Coordinates) >= 3 {
		eq.Longitude = fmt.Sprintf("%.4f", f.Geometry.Coordinates[0])
		eq.Latitude = fmt.Sprintf("%.4f", f.Geometry.Coordinates[1])
		eq.Depth = fmt.Sprintf("%.1f", f.Geometry.Coordinates[2])
	} else if len(f.Geometry.Coordinates) == 2 {
		eq.Longitude = fmt.Sprintf("%.4f", f.Geometry.Coordinates[0])
		eq.Latitude = fmt.Sprintf("%.4f", f.Geometry.Coordinates[1])
	}
	return eq
}
