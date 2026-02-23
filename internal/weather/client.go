package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
)

const defaultBaseURL = "https://api.openweathermap.org"

// Client fetches weather forecasts from the OpenWeather API.
type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

// Forecast holds a simplified weather summary for a single day.
type Forecast struct {
	Date         time.Time
	TempHighF    float64
	TempLowF     float64
	Conditions   string
	PrecipChance int    // 0-100
	Sunrise      string // "HH:MM" local
	Sunset       string // "HH:MM" local
}

// New creates a weather client. Returns nil if apiKey is empty.
func New(apiKey string) *Client {
	if apiKey == "" {
		return nil
	}
	return &Client{
		apiKey:  apiKey,
		baseURL: defaultBaseURL,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// NewWithBaseURL creates a weather client with a custom base URL (for tests).
func NewWithBaseURL(apiKey, baseURL string) *Client {
	c := New(apiKey)
	if c != nil {
		c.baseURL = baseURL
	}
	return c
}

// FetchForecast retrieves tomorrow's weather forecast for the given coordinates.
// Errors are returned to the caller — the pipeline treats weather as soft-fail.
func (c *Client) FetchForecast(ctx context.Context, lat, lon float64) (Forecast, error) {
	reqURL := fmt.Sprintf("%s/data/2.5/forecast?lat=%f&lon=%f&units=imperial",
		c.baseURL, lat, lon)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return Forecast{}, fmt.Errorf("creating weather request: %w", err)
	}

	// Add API key via query params rather than embedding in the URL string,
	// so it won't leak in error messages from http.Client.
	q := req.URL.Query()
	q.Set("appid", c.apiKey)
	req.URL.RawQuery = q.Encode()

	resp, err := c.http.Do(req)
	if err != nil {
		return Forecast{}, fmt.Errorf("fetching weather: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		io.Copy(io.Discard, resp.Body)
		return Forecast{}, fmt.Errorf("weather API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return Forecast{}, fmt.Errorf("reading weather response: %w", err)
	}

	return parseForecast(body, time.Now().UTC())
}

// openWeatherResponse models the relevant parts of the 5-day/3-hour forecast API.
type openWeatherResponse struct {
	List []forecastEntry `json:"list"`
	City struct {
		Sunrise int64 `json:"sunrise"`
		Sunset  int64 `json:"sunset"`
	} `json:"city"`
}

type forecastEntry struct {
	Dt   int64 `json:"dt"`
	Main struct {
		TempMin float64 `json:"temp_min"`
		TempMax float64 `json:"temp_max"`
	} `json:"main"`
	Weather []struct {
		Main string `json:"main"`
	} `json:"weather"`
	Pop float64 `json:"pop"` // probability of precipitation 0-1
}

func parseForecast(data []byte, referenceTime time.Time) (Forecast, error) {
	var resp openWeatherResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return Forecast{}, fmt.Errorf("parsing weather JSON: %w", err)
	}

	if len(resp.List) == 0 {
		return Forecast{}, nil
	}

	// Find tomorrow's entries relative to the reference time.
	tomorrowStart := time.Date(referenceTime.Year(), referenceTime.Month(), referenceTime.Day()+1, 0, 0, 0, 0, time.UTC)
	tomorrowEnd := tomorrowStart.AddDate(0, 0, 1)

	var (
		highTemp   = -math.MaxFloat64
		lowTemp    = math.MaxFloat64
		maxPrecip  float64
		conditions = make(map[string]int)
	)
	found := false

	for _, entry := range resp.List {
		entryTime := time.Unix(entry.Dt, 0).UTC()
		if entryTime.Before(tomorrowStart) || !entryTime.Before(tomorrowEnd) {
			continue
		}
		found = true

		if entry.Main.TempMax > highTemp {
			highTemp = entry.Main.TempMax
		}
		if entry.Main.TempMin < lowTemp {
			lowTemp = entry.Main.TempMin
		}
		if entry.Pop > maxPrecip {
			maxPrecip = entry.Pop
		}
		if len(entry.Weather) > 0 {
			conditions[entry.Weather[0].Main]++
		}
	}

	if !found {
		return Forecast{}, nil
	}

	// Dominant condition: most frequent weather main.
	dominant := ""
	maxCount := 0
	for cond, count := range conditions {
		if count > maxCount {
			maxCount = count
			dominant = cond
		}
	}

	sunrise := time.Unix(resp.City.Sunrise, 0).UTC().Format("15:04")
	sunset := time.Unix(resp.City.Sunset, 0).UTC().Format("15:04")

	return Forecast{
		Date:         tomorrowStart,
		TempHighF:    math.Round(highTemp*10) / 10,
		TempLowF:     math.Round(lowTemp*10) / 10,
		Conditions:   dominant,
		PrecipChance: int(math.Round(maxPrecip * 100)),
		Sunrise:      sunrise,
		Sunset:       sunset,
	}, nil
}
