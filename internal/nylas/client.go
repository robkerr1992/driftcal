package nylas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/sethvargo/go-retry"
	"golang.org/x/time/rate"
)

const defaultBaseURL = "https://api.us.nylas.com"

// Client is a Nylas v3 API HTTP client.
type Client struct {
	clientID string
	apiKey   string
	http     *http.Client
	baseURL  string
	limiter  *rate.Limiter
}

// New creates a Nylas client configured for the production API.
func New(clientID, apiKey string) *Client {
	return &Client{
		clientID: clientID,
		apiKey:   apiKey,
		http:     &http.Client{Timeout: 30 * time.Second},
		baseURL:  defaultBaseURL,
		limiter:  rate.NewLimiter(rate.Limit(5), 1), // 5 req/s, burst 1
	}
}

// NewWithBaseURL creates a Nylas client with a custom base URL (for tests).
func NewWithBaseURL(clientID, apiKey, baseURL string) *Client {
	c := New(clientID, apiKey)
	c.baseURL = baseURL
	return c
}

// AuthURL returns the Nylas hosted authentication URL that the user should
// visit to connect their calendar account.
func (c *Client) AuthURL(redirectURI, provider, state string) string {
	v := url.Values{
		"client_id":     {c.clientID},
		"redirect_uri":  {redirectURI},
		"provider":      {provider},
		"response_type": {"code"},
		"state":         {state},
	}
	return c.baseURL + "/v3/connect/auth?" + v.Encode()
}

// ExchangeCode exchanges an authorization code for grant credentials.
func (c *Client) ExchangeCode(ctx context.Context, code, redirectURI string) (*TokenResponse, error) {
	body := tokenExchangeRequest{
		ClientID:    c.clientID,
		Code:        code,
		RedirectURI: redirectURI,
		GrantType:   "authorization_code",
	}

	var result TokenResponse
	if err := c.post(ctx, "/v3/connect/token", body, &result); err != nil {
		return nil, fmt.Errorf("exchanging auth code: %w", err)
	}
	return &result, nil
}

// ListCalendars returns all calendars for the given grant.
func (c *Client) ListCalendars(ctx context.Context, grantID string) ([]Calendar, error) {
	path := fmt.Sprintf("/v3/grants/%s/calendars", grantID)

	var resp listResponse[Calendar]
	if err := c.get(ctx, path, nil, &resp); err != nil {
		return nil, fmt.Errorf("listing calendars: %w", err)
	}
	return resp.Data, nil
}

// ListEvents returns all events for a calendar within the given time range.
// It handles pagination automatically.
func (c *Client) ListEvents(ctx context.Context, grantID, calendarID string, start, end time.Time) ([]Event, error) {
	path := fmt.Sprintf("/v3/grants/%s/events", grantID)
	params := url.Values{
		"calendar_id": {calendarID},
		"start":       {strconv.FormatInt(start.Unix(), 10)},
		"end":         {strconv.FormatInt(end.Unix(), 10)},
		"limit":       {"200"},
	}

	const maxPages = 50

	var all []Event
	for page := 0; ; page++ {
		if page >= maxPages {
			return all, fmt.Errorf("pagination limit reached (%d pages)", maxPages)
		}

		var resp listResponse[Event]
		if err := c.get(ctx, path, params, &resp); err != nil {
			return nil, fmt.Errorf("listing events: %w", err)
		}
		all = append(all, resp.Data...)

		if resp.NextCursor == "" {
			break
		}
		params.Set("page_token", resp.NextCursor)
	}
	return all, nil
}

// GetEvent fetches a single event by ID.
func (c *Client) GetEvent(ctx context.Context, grantID, eventID, calendarID string) (*Event, error) {
	path := fmt.Sprintf("/v3/grants/%s/events/%s", grantID, eventID)
	params := url.Values{"calendar_id": {calendarID}}

	var resp struct {
		Data Event `json:"data"`
	}
	if err := c.get(ctx, path, params, &resp); err != nil {
		return nil, fmt.Errorf("getting event %s: %w", eventID, err)
	}
	return &resp.Data, nil
}

// CreateEvent creates a new event on the specified calendar.
func (c *Client) CreateEvent(ctx context.Context, grantID, calendarID string, req CreateEventRequest) (*Event, error) {
	path := fmt.Sprintf("/v3/grants/%s/events?calendar_id=%s", grantID, url.QueryEscape(calendarID))

	var resp struct {
		Data Event `json:"data"`
	}
	if err := c.post(ctx, path, req, &resp); err != nil {
		return nil, fmt.Errorf("creating event: %w", err)
	}
	return &resp.Data, nil
}

// CreateCalendar creates a new calendar on the specified grant.
func (c *Client) CreateCalendar(ctx context.Context, grantID string, req CreateCalendarRequest) (*Calendar, error) {
	path := fmt.Sprintf("/v3/grants/%s/calendars", grantID)
	var resp struct {
		Data Calendar `json:"data"`
	}
	if err := c.post(ctx, path, req, &resp); err != nil {
		return nil, fmt.Errorf("creating calendar: %w", err)
	}
	return &resp.Data, nil
}

// get performs an authenticated GET request and decodes the JSON response.
func (c *Client) get(ctx context.Context, path string, params url.Values, dest any) error {
	u := c.baseURL + path
	if params != nil {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	return c.do(req, dest)
}

// post performs an authenticated POST request with a JSON body.
func (c *Client) post(ctx context.Context, path string, body, dest any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshaling request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	return c.do(req, dest)
}

// do executes the request with auth headers, retry on transient errors,
// and handles response parsing.
func (c *Client) do(req *http.Request, dest any) error {
	if err := c.limiter.Wait(req.Context()); err != nil {
		return fmt.Errorf("rate limiter: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	b := retry.NewExponential(2 * time.Second)
	b = retry.WithMaxRetries(3, b)

	return retry.Do(req.Context(), b, func(ctx context.Context) error {
		resp, err := c.http.Do(req.WithContext(ctx))
		if err != nil {
			return retry.RetryableError(fmt.Errorf("executing request: %w", err))
		}
		defer resp.Body.Close()

		const maxResponseBytes = 10 << 20 // 10 MB
		respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
		if err != nil {
			return fmt.Errorf("reading response body: %w", err)
		}
		if int64(len(respBody)) > maxResponseBytes {
			return fmt.Errorf("response body exceeded %d bytes", maxResponseBytes)
		}

		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			return retry.RetryableError(fmt.Errorf("nylas API error (status %d): %s", resp.StatusCode, string(respBody)))
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("nylas API error (status %d): %s", resp.StatusCode, string(respBody))
		}

		if dest != nil {
			if err := json.Unmarshal(respBody, dest); err != nil {
				return fmt.Errorf("decoding response: %w", err)
			}
		}
		return nil
	})
}
