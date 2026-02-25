package nylas

import "encoding/json"

// TokenResponse is returned by Nylas after exchanging an authorization code.
type TokenResponse struct {
	GrantID  string `json:"grant_id"`
	Email    string `json:"email"`
	Provider string `json:"provider"`
}

// Calendar represents a Nylas calendar object.
type Calendar struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	HexColor  string `json:"hex_color"`
	IsPrimary bool   `json:"is_primary"`
}

// Event represents a Nylas event object.
type Event struct {
	ID             string    `json:"id"`
	CalendarID     string    `json:"calendar_id"`
	Title          string    `json:"title"`
	Description    string    `json:"description"`
	Location       string    `json:"location"`
	Status         string    `json:"status"`
	Busy           bool      `json:"busy"`
	When           EventWhen `json:"when"`
	RecurrenceRule []string  `json:"recurrence"`
}

// EventWhen represents the polymorphic time representation for Nylas events.
// The Object field discriminates: "timespan", "date", or "datespan".
type EventWhen struct {
	Object        string `json:"object"`
	StartTime     int64  `json:"start_time"`      // timespan: unix timestamp
	EndTime       int64  `json:"end_time"`         // timespan: unix timestamp
	StartTimezone string `json:"start_timezone"`   // IANA timezone
	EndTimezone   string `json:"end_timezone"`     // IANA timezone
	Date          string `json:"date"`             // date: YYYY-MM-DD
	StartDate     string `json:"start_date"`       // datespan: YYYY-MM-DD
	EndDate       string `json:"end_date"`         // datespan: YYYY-MM-DD
}

// WebhookPayload represents an incoming Nylas webhook notification.
type WebhookPayload struct {
	Type string          `json:"type"`
	Data WebhookData     `json:"data"`
}

// WebhookData contains the webhook event details.
type WebhookData struct {
	Object    json.RawMessage `json:"object"`
	ObjectID  string          `json:"object_data_id,omitempty"`
}

// listResponse wraps paginated list endpoints.
type listResponse[T any] struct {
	Data       []T    `json:"data"`
	NextCursor string `json:"next_cursor"`
}

// CreateCalendarRequest is the body sent to create a calendar on a grant.
type CreateCalendarRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// CreateEventRequest is the body sent to create a calendar event.
type CreateEventRequest struct {
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	When        EventWhen `json:"when"`
	Busy        bool     `json:"busy"`
}

// tokenExchangeRequest is the body sent to exchange an auth code.
type tokenExchangeRequest struct {
	ClientID    string `json:"client_id"`
	Code        string `json:"code"`
	RedirectURI string `json:"redirect_uri"`
	GrantType   string `json:"grant_type"`
}
