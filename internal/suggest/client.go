package suggest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sethvargo/go-retry"
)

const (
	defaultBaseURL = "https://api.anthropic.com"
	defaultModel   = "claude-sonnet-4-6"
)

// Client calls the Anthropic Messages API to generate activity suggestions.
type Client struct {
	apiKey  string
	baseURL string
	model   string
	http    *http.Client
}

// Suggestion represents a single activity suggestion from the LLM.
type Suggestion struct {
	GapNumber     int    `json:"gap_number"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	Category      string `json:"category"`
	EnergyLevel   string `json:"energy_level"`
	EstimatedCost string `json:"estimated_cost"`
	Location      string `json:"location"`
	LocationURL   string `json:"location_url"`
	Reasoning     string `json:"reasoning"`
}

// GenerateResult holds the parsed suggestions and usage metadata.
type GenerateResult struct {
	Suggestions  []Suggestion
	RequestID    string
	InputTokens  int
	OutputTokens int
}

// New creates a suggestion client. Returns nil if apiKey is empty.
// If model is empty, the default model is used.
func New(apiKey, model string) *Client {
	if apiKey == "" {
		return nil
	}
	if model == "" {
		model = defaultModel
	}
	return &Client{
		apiKey:  apiKey,
		baseURL: defaultBaseURL,
		model:   model,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

// NewWithBaseURL creates a suggestion client with a custom base URL (for tests).
func NewWithBaseURL(apiKey, baseURL string) *Client {
	c := New(apiKey, "")
	if c != nil {
		c.baseURL = baseURL
	}
	return c
}

// Generate calls the Anthropic Messages API with the suggest_activities tool
// and returns parsed suggestions. Uses tool_choice to force tool use.
func (c *Client) Generate(ctx context.Context, systemPrompt, userPrompt string) (*GenerateResult, error) {
	return c.callAPI(ctx, systemPrompt, userPrompt, 0.9)
}

func (c *Client) callAPI(ctx context.Context, systemPrompt, userPrompt string, temperature float64) (*GenerateResult, error) {
	reqBody := messagesRequest{
		Model:       c.model,
		MaxTokens:   2000,
		Temperature: temperature,
		System:      systemPrompt,
		Messages: []message{
			{Role: "user", Content: userPrompt},
		},
		Tools:      []tool{suggestActivitiesTool()},
		ToolChoice: &toolChoice{Type: "tool", Name: "suggest_activities"},
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	var result *GenerateResult

	b := retry.NewExponential(2 * time.Second)
	b = retry.WithMaxRetries(3, b)

	err = retry.Do(ctx, b, func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("creating request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", c.apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")

		resp, err := c.http.Do(req)
		if err != nil {
			return retry.RetryableError(fmt.Errorf("executing request: %w", err))
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return fmt.Errorf("reading response: %w", err)
		}

		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			return retry.RetryableError(fmt.Errorf("anthropic API error (status %d): %s", resp.StatusCode, string(body)))
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("anthropic API error (status %d): %s", resp.StatusCode, string(body))
		}

		var messagesResp messagesResponse
		if err := json.Unmarshal(body, &messagesResp); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}

		result = &GenerateResult{
			RequestID:    resp.Header.Get("x-request-id"),
			InputTokens:  messagesResp.Usage.InputTokens,
			OutputTokens: messagesResp.Usage.OutputTokens,
		}

		// Find the tool_use block.
		for _, block := range messagesResp.Content {
			if block.Type == "tool_use" && block.Name == "suggest_activities" {
				var toolInput struct {
					Suggestions []Suggestion `json:"suggestions"`
				}
				if err := json.Unmarshal(block.Input, &toolInput); err != nil {
					return fmt.Errorf("parsing tool input: %w", err)
				}
				result.Suggestions = toolInput.Suggestions
				break
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return result, nil
}

// --- API types ---

type messagesRequest struct {
	Model       string      `json:"model"`
	MaxTokens   int         `json:"max_tokens"`
	Temperature float64     `json:"temperature"`
	System      string      `json:"system"`
	Messages    []message   `json:"messages"`
	Tools       []tool      `json:"tools"`
	ToolChoice  *toolChoice `json:"tool_choice,omitempty"`
}

type toolChoice struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type messagesResponse struct {
	Content []contentBlock `json:"content"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type contentBlock struct {
	Type  string          `json:"type"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

func suggestActivitiesTool() tool {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"suggestions": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"gap_number": {"type": "integer"},
						"title": {"type": "string"},
						"description": {"type": "string"},
						"category": {"type": "string", "enum": ["outdoor", "cultural", "social", "fitness", "creative", "culinary", "relaxation"]},
						"energy_level": {"type": "string", "enum": ["low", "medium", "high"]},
						"estimated_cost": {"type": "string", "enum": ["free", "$", "$$", "$$$"]},
						"location": {"type": "string"},
						"location_url": {"type": ["string", "null"]},
						"reasoning": {"type": "string"}
					},
					"required": ["gap_number", "title", "description", "category", "energy_level", "estimated_cost", "location", "reasoning"]
				}
			}
		},
		"required": ["suggestions"]
	}`)
	return tool{
		Name:        "suggest_activities",
		Description: "Generate activity suggestions for free calendar gaps",
		InputSchema: schema,
	}
}
