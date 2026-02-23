package weather

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func tomorrowEntries() openWeatherResponse {
	now := time.Now().UTC()
	tomorrowNoon := time.Date(now.Year(), now.Month(), now.Day()+1, 12, 0, 0, 0, time.UTC)
	tomorrowEvening := time.Date(now.Year(), now.Month(), now.Day()+1, 18, 0, 0, 0, time.UTC)

	return openWeatherResponse{
		List: []forecastEntry{
			{
				Dt: tomorrowNoon.Unix(),
				Main: struct {
					TempMin float64 `json:"temp_min"`
					TempMax float64 `json:"temp_max"`
				}{TempMin: 45.2, TempMax: 62.8},
				Weather: []struct {
					Main string `json:"main"`
				}{{Main: "Clouds"}},
				Pop: 0.3,
			},
			{
				Dt: tomorrowEvening.Unix(),
				Main: struct {
					TempMin float64 `json:"temp_min"`
					TempMax float64 `json:"temp_max"`
				}{TempMin: 40.1, TempMax: 55.0},
				Weather: []struct {
					Main string `json:"main"`
				}{{Main: "Clouds"}},
				Pop: 0.6,
			},
		},
		City: struct {
			Sunrise int64 `json:"sunrise"`
			Sunset  int64 `json:"sunset"`
		}{
			Sunrise: tomorrowNoon.Add(-5 * time.Hour).Unix(),
			Sunset:  tomorrowNoon.Add(5 * time.Hour).Unix(),
		},
	}
}

func TestFetchForecast_Success(t *testing.T) {
	resp := tomorrowEntries()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/data/2.5/forecast" {
			t.Errorf("path = %s, want /data/2.5/forecast", r.URL.Path)
		}
		if r.URL.Query().Get("units") != "imperial" {
			t.Error("expected units=imperial")
		}
		if r.URL.Query().Get("appid") != "test-key" {
			t.Error("expected appid=test-key")
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewWithBaseURL("test-key", srv.URL)
	forecast, err := c.FetchForecast(context.Background(), 38.9, -77.0)
	if err != nil {
		t.Fatalf("FetchForecast failed: %v", err)
	}

	if forecast.TempHighF != 62.8 {
		t.Errorf("TempHighF = %f, want 62.8", forecast.TempHighF)
	}
	if forecast.TempLowF != 40.1 {
		t.Errorf("TempLowF = %f, want 40.1", forecast.TempLowF)
	}
	if forecast.Conditions != "Clouds" {
		t.Errorf("Conditions = %q, want Clouds", forecast.Conditions)
	}
	if forecast.PrecipChance != 60 {
		t.Errorf("PrecipChance = %d, want 60", forecast.PrecipChance)
	}
	if forecast.Sunrise == "" {
		t.Error("Sunrise should not be empty")
	}
	if forecast.Sunset == "" {
		t.Error("Sunset should not be empty")
	}
}

func TestFetchForecast_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"cod":401,"message":"Invalid API key"}`))
	}))
	defer srv.Close()

	c := NewWithBaseURL("bad-key", srv.URL)
	forecast, err := c.FetchForecast(context.Background(), 38.9, -77.0)
	if err != nil {
		t.Fatalf("expected nil error for API failure (graceful degradation), got: %v", err)
	}
	// Zero forecast.
	if forecast.Conditions != "" {
		t.Errorf("Conditions = %q, want empty (zero forecast)", forecast.Conditions)
	}
	if forecast.TempHighF != 0 {
		t.Errorf("TempHighF = %f, want 0", forecast.TempHighF)
	}
}

func TestNew_EmptyKey(t *testing.T) {
	c := New("")
	if c != nil {
		t.Error("New(\"\") should return nil")
	}
}

func TestFetchForecast_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json at all`))
	}))
	defer srv.Close()

	c := NewWithBaseURL("test-key", srv.URL)
	forecast, err := c.FetchForecast(context.Background(), 38.9, -77.0)
	if err != nil {
		t.Fatalf("expected nil error for malformed JSON, got: %v", err)
	}
	if forecast.Conditions != "" {
		t.Error("expected zero forecast for malformed JSON")
	}
}
