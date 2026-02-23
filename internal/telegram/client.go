package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/time/rate"
)

const defaultBaseURL = "https://api.telegram.org"

// Client is a raw HTTP client for the Telegram Bot API.
// It supports SendMessage, EditMessageText, AnswerCallbackQuery, and SetWebhook.
type Client struct {
	token   string
	baseURL string
	http    *http.Client
	limiter *rate.Limiter
}

// New creates a Client with default settings.
func New(token string) *Client {
	return NewWithBaseURL(token, defaultBaseURL)
}

// NewWithBaseURL creates a Client targeting a custom base URL (for tests).
func NewWithBaseURL(token, baseURL string) *Client {
	return &Client{
		token:   token,
		baseURL: baseURL,
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
		limiter: rate.NewLimiter(30, 5), // Telegram limit: 30 req/s, burst 5
	}
}

// SendOpts holds optional parameters for SendMessage.
type SendOpts struct {
	ParseMode   string           `json:"parse_mode,omitempty"`
	ReplyMarkup *InlineKeyboard  `json:"reply_markup,omitempty"`
}

// InlineButton represents one button in an inline keyboard row.
type InlineButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
}

// InlineKeyboard represents the inline keyboard markup for Telegram messages.
type InlineKeyboard struct {
	InlineKeyboard [][]InlineButton `json:"inline_keyboard"`
}

// NewInlineKeyboard builds an InlineKeyboard from rows of buttons.
func NewInlineKeyboard(rows ...[]InlineButton) *InlineKeyboard {
	return &InlineKeyboard{InlineKeyboard: rows}
}

// SendMessage sends a text message to a chat and returns the message ID.
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string, opts *SendOpts) (int, error) {
	payload := map[string]any{
		"chat_id": chatID,
		"text":    text,
	}
	if opts != nil {
		if opts.ParseMode != "" {
			payload["parse_mode"] = opts.ParseMode
		}
		if opts.ReplyMarkup != nil {
			payload["reply_markup"] = opts.ReplyMarkup
		}
	}

	var result struct {
		OK     bool `json:"ok"`
		Result struct {
			MessageID int `json:"message_id"`
		} `json:"result"`
		Description string `json:"description"`
	}
	if err := c.call(ctx, "sendMessage", payload, &result); err != nil {
		return 0, err
	}
	if !result.OK {
		return 0, fmt.Errorf("sendMessage failed: %s", result.Description)
	}
	return result.Result.MessageID, nil
}

// EditMessageText edits the text (and optionally keyboard) of an existing message.
func (c *Client) EditMessageText(ctx context.Context, chatID int64, messageID int, text string, opts *SendOpts) error {
	payload := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
		"text":       text,
	}
	if opts != nil {
		if opts.ParseMode != "" {
			payload["parse_mode"] = opts.ParseMode
		}
		if opts.ReplyMarkup != nil {
			payload["reply_markup"] = opts.ReplyMarkup
		}
	}

	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := c.call(ctx, "editMessageText", payload, &result); err != nil {
		return err
	}
	if !result.OK {
		return fmt.Errorf("editMessageText failed: %s", result.Description)
	}
	return nil
}

// AnswerCallbackQuery dismisses the loading spinner on a callback query.
func (c *Client) AnswerCallbackQuery(ctx context.Context, callbackQueryID, text string) error {
	payload := map[string]any{
		"callback_query_id": callbackQueryID,
	}
	if text != "" {
		payload["text"] = text
	}

	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := c.call(ctx, "answerCallbackQuery", payload, &result); err != nil {
		return err
	}
	if !result.OK {
		return fmt.Errorf("answerCallbackQuery failed: %s", result.Description)
	}
	return nil
}

// SetWebhook registers a webhook URL with Telegram.
func (c *Client) SetWebhook(ctx context.Context, url, secret string) error {
	payload := map[string]any{
		"url":          url,
		"secret_token": secret,
	}

	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := c.call(ctx, "setWebhook", payload, &result); err != nil {
		return err
	}
	if !result.OK {
		return fmt.Errorf("setWebhook failed: %s", result.Description)
	}
	return nil
}

func (c *Client) call(ctx context.Context, method string, payload any, dest any) error {
	if err := c.limiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limit: %w", err)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	url := fmt.Sprintf("%s/bot%s/%s", c.baseURL, c.token, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("telegram API %s returned %d: %s", method, resp.StatusCode, string(respBody))
	}

	if err := json.Unmarshal(respBody, dest); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	return nil
}
