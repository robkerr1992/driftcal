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
)

const defaultBaseURL = "https://api.us.nylas.com"

// Client is a Nylas v3 API HTTP client.
type Client struct {
	clientID string
	apiKey   string
	http     *http.Client
	baseURL  string
}

// New creates a Nylas client configured for the production API.
func New(clientID, apiKey string) *Client {
	return &Client{
		clientID: clientID,
		apiKey:   apiKey,
		http:     &http.Client{Timeout: 30 * time.Second},
		baseURL:  defaultBaseURL,
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
func (c *Client) AuthURL(redirectURI, provider string) string {
	v := url.Values{
		"client_id":    {c.clientID},
		"redirect_uri": {redirectURI},
		"provider":     {provider},
		"response_type": {"code"},
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

	var all []Event
	for {
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

// do executes the request with auth headers and handles response parsing.
func (c *Client) do(req *http.Request, dest any) error {
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
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
}
