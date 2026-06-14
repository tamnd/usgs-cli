package usgs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// sampleGeoJSON is a minimal USGS GeoJSON FeatureCollection response.
const sampleGeoJSON = `{
  "type": "FeatureCollection",
  "features": [
    {
      "type": "Feature",
      "id": "us7000m6al",
      "properties": {
        "mag": 5.1,
        "place": "27 km SE of Magdalena, Philippines",
        "time": 1718231234000,
        "type": "earthquake"
      },
      "geometry": {
        "type": "Point",
        "coordinates": [122.1234, 14.5678, 10.5]
      }
    },
    {
      "type": "Feature",
      "id": "us7000m6am",
      "properties": {
        "mag": 6.3,
        "place": "Southern Mindanao, Philippines",
        "time": 1718230000000,
        "type": "earthquake"
      },
      "geometry": {
        "type": "Point",
        "coordinates": [125.5678, 6.1234, 35.0]
      }
    }
  ]
}`

func newTestClient(srv *httptest.Server) *Client {
	cfg := DefaultConfig()
	cfg.BaseURL = srv.URL + "/fdsnws/event/1"
	cfg.Rate = 0
	cfg.Retries = 0
	cfg.Timeout = 5 * time.Second
	return NewClient(cfg)
}

func TestQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fdsnws/event/1/query" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if ua := r.Header.Get("User-Agent"); ua == "" {
			t.Error("request carried no User-Agent")
		}
		if r.URL.Query().Get("format") != "geojson" {
			t.Error("missing format=geojson param")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleGeoJSON))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	eqs, err := c.Query(context.Background(), QueryParams{
		Start:  "2026-06-01",
		MinMag: "5.0",
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(eqs) != 2 {
		t.Fatalf("got %d earthquakes, want 2", len(eqs))
	}

	eq := eqs[0]
	if eq.Place != "27 km SE of Magdalena, Philippines" {
		t.Errorf("Place = %q", eq.Place)
	}
	if eq.Magnitude != "5.1" {
		t.Errorf("Magnitude = %q, want 5.1", eq.Magnitude)
	}
	if eq.Depth != "10.5" {
		t.Errorf("Depth = %q, want 10.5", eq.Depth)
	}
	if eq.Latitude != "14.5678" {
		t.Errorf("Latitude = %q, want 14.5678", eq.Latitude)
	}
	if eq.Longitude != "122.1234" {
		t.Errorf("Longitude = %q, want 122.1234", eq.Longitude)
	}
	if eq.Type != "earthquake" {
		t.Errorf("Type = %q, want earthquake", eq.Type)
	}
	if eq.Time == "" {
		t.Error("Time is empty")
	}
}

func TestRecent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("minmagnitude") != "6.0" {
			t.Errorf("minmagnitude = %q, want 6.0", q.Get("minmagnitude"))
		}
		if q.Get("limit") != "50" {
			t.Errorf("limit = %q, want 50", q.Get("limit"))
		}
		if q.Get("orderby") != "time" {
			t.Errorf("orderby = %q, want time", q.Get("orderby"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleGeoJSON))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	eqs, err := c.Recent(context.Background())
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(eqs) == 0 {
		t.Error("Recent returned no earthquakes")
	}
}

func TestQueryRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"type":"FeatureCollection","features":[]}`))
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.BaseURL = srv.URL + "/fdsnws/event/1"
	cfg.Rate = 0
	cfg.Retries = 5
	cfg.Timeout = 10 * time.Second
	c := NewClient(cfg)

	_, err := c.Query(context.Background(), QueryParams{MinMag: "5.0"})
	if err != nil {
		t.Fatalf("Query after retries: %v", err)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
}

func TestConvertFeatureTimeFormat(t *testing.T) {
	// 1718231234000 ms = 2024-06-12T19:47:14Z
	f := rawFeature{
		Properties: rawProperties{
			Mag:   7.2,
			Place: "Test location",
			Time:  1718231234000,
			Type:  "earthquake",
		},
		Geometry: rawGeometry{
			Coordinates: []float64{-118.4065, 34.0901, 8.0},
		},
	}
	eq := convertFeature(f)
	if eq.Time == "" {
		t.Error("Time is empty")
	}
	// Verify RFC3339 format
	if _, err := time.Parse(time.RFC3339, eq.Time); err != nil {
		t.Errorf("Time %q is not RFC3339: %v", eq.Time, err)
	}
	if eq.Magnitude != "7.2" {
		t.Errorf("Magnitude = %q, want 7.2", eq.Magnitude)
	}
	if eq.Depth != "8.0" {
		t.Errorf("Depth = %q, want 8.0", eq.Depth)
	}
}
