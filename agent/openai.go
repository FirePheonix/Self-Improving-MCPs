package agent

import (
	"context"
	"fmt"
	"os"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// Client is a thin OpenAI wrapper for offline agents.
type Client struct {
	api   openai.Client
	Label string // cheap model (nano)
	Reason string // evaluate/propose model (mini)
}

// NewClient builds a client from OPENAI_API_KEY and model names.
func NewClient(labelModel, reasonModel string) (*Client, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is not set")
	}
	if labelModel == "" {
		labelModel = "gpt-4.1-nano"
	}
	if reasonModel == "" {
		reasonModel = "gpt-4.1-mini"
	}
	return &Client{
		api:    openai.NewClient(option.WithAPIKey(key)),
		Label:  labelModel,
		Reason: reasonModel,
	}, nil
}

// Complete sends a simple system+user chat completion and returns text.
func (c *Client) Complete(ctx context.Context, model, system, user string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("openai client is nil")
	}
	if model == "" {
		model = c.Reason
	}
	res, err := c.api.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(system),
			openai.UserMessage(user),
		},
	})
	if err != nil {
		return "", fmt.Errorf("openai complete: %w", err)
	}
	if len(res.Choices) == 0 {
		return "", fmt.Errorf("openai complete: empty choices")
	}
	return res.Choices[0].Message.Content, nil
}
