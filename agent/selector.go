package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	selectiongate "self-improving-mcp/selection-gate"
)

// DefaultSelectorSystem is a generic, domain-free prompt for tool selection.
const DefaultSelectorSystem = "You are an agent with access to MCP tools. " +
	"Pick exactly one best tool for the user request. Prefer the most specific tool. Always call a tool."

// OpenAISelector lets the selection gate use an OpenAI model as the agent under test.
type OpenAISelector struct {
	Client *Client
	Model  string // chat model; empty uses Client.Reason
	System string // system prompt; empty uses DefaultSelectorSystem
}

var _ selectiongate.Selector = OpenAISelector{}

// Select asks the model to call one tool for the query and returns its name ("" if it called none).
func (s OpenAISelector) Select(ctx context.Context, query string, tools []selectiongate.Tool) (string, error) {
	if s.Client == nil {
		return "", fmt.Errorf("openai selector: client is nil")
	}
	system := s.System
	if system == "" {
		system = DefaultSelectorSystem
	}
	specs := make([]ToolSpec, len(tools))
	for i, t := range tools {
		specs[i] = ToolSpec{Name: t.Name, Description: t.Description, Parameters: t.Parameters}
	}
	call, _, err := s.Client.CompleteWithTools(ctx, s.Model, system, query, specs)
	if err != nil {
		return "", err
	}
	return call.Name, nil
}

var _ selectiongate.CallSelector = OpenAISelector{}

// SelectCall is Select plus the arguments the model built, so the gate can check the call could run.
// Arguments that are not a JSON object are reported in ArgsInvalid, so the pick cannot count as correct.
func (s OpenAISelector) SelectCall(ctx context.Context, query string, tools []selectiongate.Tool) (selectiongate.Call, error) {
	if s.Client == nil {
		return selectiongate.Call{}, fmt.Errorf("openai selector: client is nil")
	}
	system := s.System
	if system == "" {
		system = DefaultSelectorSystem
	}
	specs := make([]ToolSpec, len(tools))
	for i, t := range tools {
		specs[i] = ToolSpec{Name: t.Name, Description: t.Description, Parameters: t.Parameters}
	}
	call, _, err := s.Client.CompleteWithTools(ctx, s.Model, system, query, specs)
	if err != nil {
		return selectiongate.Call{}, err
	}
	out := selectiongate.Call{Name: call.Name}
	if call.Name == "" {
		return out, nil
	}
	args := map[string]any{}
	if strings.TrimSpace(call.Arguments) != "" {
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			return selectiongate.Call{Name: call.Name, ArgsInvalid: "arguments are not a JSON object: " + err.Error()}, nil
		}
	}
	out.Args = args
	return out, nil
}
