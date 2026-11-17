package agent

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// Client is a thin OpenAI wrapper for offline agents.
type Client struct {
	api    openai.Client
	Label  string // cheap model (nano)
	Reason string // evaluate/propose model (mini)
	// Temperature, when set, is sent with tool-selection requests. Evaluations that compare two
	// conditions should set it to 0 so sampling noise does not masquerade as a difference.
	Temperature *float64
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
	opts := []option.RequestOption{option.WithAPIKey(key)}
	// OPENAI_BASE_URL lets tests point the offline agent at a local stub (see scripts/subagent-oracle).
	if base := os.Getenv("OPENAI_BASE_URL"); base != "" {
		opts = append(opts, option.WithBaseURL(base))
	}
	// OPENAI_MAX_RETRIES raises the SDK's retry count (default 2) for providers with tight per-minute
	// limits; the SDK waits as the server's Retry-After asks. Daily-quota errors are not retried (fatal.go).
	if n, err := strconv.Atoi(os.Getenv("OPENAI_MAX_RETRIES")); err == nil && n >= 0 {
		opts = append(opts, option.WithMaxRetries(n))
	}
	return &Client{
		api:    openai.NewClient(opts...),
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
		return "", classify(fmt.Errorf("openai complete: %w", err))
	}
	if len(res.Choices) == 0 {
		return "", fmt.Errorf("openai complete: empty choices")
	}
	return res.Choices[0].Message.Content, nil
}

// CompleteJSON is Complete, but asks the API to return a JSON object (response_format=json_object).
// Used where the caller parses structured output, so a stray character cannot silently void the result.
func (c *Client) CompleteJSON(ctx context.Context, model, system, user string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("openai client is nil")
	}
	if model == "" {
		model = c.Reason
	}
	req := openai.ChatCompletionNewParams{
		Model: model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(system),
			openai.UserMessage(user),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &openai.ResponseFormatJSONObjectParam{},
		},
	}
	if c.Temperature != nil {
		req.Temperature = openai.Float(*c.Temperature) // judges must be as repeatable as the selector
	}
	res, err := c.api.Chat.Completions.New(ctx, req)
	if err != nil {
		return "", classify(fmt.Errorf("openai complete json: %w", err))
	}
	if len(res.Choices) == 0 {
		return "", fmt.Errorf("openai complete json: empty choices")
	}
	return res.Choices[0].Message.Content, nil
}

// ToolSpec is a function tool exposed to the model.
type ToolSpec struct {
	Name        string
	Description string
	Parameters  any
}

// ToolCall is one model-requested tool invocation.
type ToolCall struct {
	Name      string
	Arguments string
}

// CompleteWithTools asks the model to call tools for a user query.
// Returns the first tool call if present.
func (c *Client) CompleteWithTools(ctx context.Context, model, system, user string, tools []ToolSpec) (ToolCall, string, error) {
	if c == nil {
		return ToolCall{}, "", fmt.Errorf("openai client is nil")
	}
	if model == "" {
		model = c.Reason
	}
	params := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, t := range tools {
		schema, ok := t.Parameters.(map[string]any)
		if !ok || schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		params = append(params, openai.ChatCompletionToolUnionParam{
			OfFunction: &openai.ChatCompletionFunctionToolParam{
				Function: openai.FunctionDefinitionParam{
					Name:        t.Name,
					Description: openai.String(t.Description),
					Parameters:  schema,
				},
			},
		})
	}
	req := openai.ChatCompletionNewParams{
		Model: model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(system),
			openai.UserMessage(user),
		},
		Tools: params,
	}
	if c.Temperature != nil {
		req.Temperature = openai.Float(*c.Temperature)
	}
	res, err := c.api.Chat.Completions.New(ctx, req)
	if err != nil {
		return ToolCall{}, "", classify(fmt.Errorf("openai tools complete: %w", err))
	}
	if len(res.Choices) == 0 {
		return ToolCall{}, "", fmt.Errorf("openai tools complete: empty choices")
	}
	msg := res.Choices[0].Message
	if len(msg.ToolCalls) == 0 {
		return ToolCall{}, msg.Content, nil
	}
	tc := msg.ToolCalls[0]
	return ToolCall{Name: tc.Function.Name, Arguments: tc.Function.Arguments}, msg.Content, nil
}
