// Package selectiongate decides whether an overlay makes an agent pick the right tool more often.
//
// The older replay sandbox checks that routed calls still run; it cannot tell whether the agent
// would have chosen well. This gate measures that directly: it shows a selector the tool list an
// overlay produces, asks it to pick for labeled sealed tasks, and compares the overlay against
// the baseline task by task. It returns pass, fail, or inconclusive; only pass may ship.
package selectiongate

import (
	"context"
	"errors"
	"fmt"
	"sync"

	improvementsandbox "self-improving-mcp/improvement-sandbox"
)

// Tool is a tool as the agent sees it.
type Tool struct {
	Name        string
	Description string
	Parameters  any
}

// Selector picks one tool for a query. Implementations must be safe for concurrent use.
type Selector interface {
	Select(ctx context.Context, query string, tools []Tool) (string, error)
}

// Call is a tool call as the agent would make it: the tool and the arguments it built.
type Call struct {
	Name string
	Args map[string]any
	// ArgsInvalid, when set, says the arguments could not even be read (for example not a JSON
	// object). Such a call cannot run, so it never counts as correct.
	ArgsInvalid string
}

// CallSelector is a Selector that also returns the arguments. When the selector implements it, the
// gate checks that the arguments are valid for the tool actually called (after routing), so a pick
// counts only if the call could run. A name-only Selector is still accepted; its outcomes are
// marked ArgsChecked=false.
type CallSelector interface {
	Selector
	SelectCall(ctx context.Context, query string, tools []Tool) (Call, error)
}

// Outcome is the result for one task under one overlay.
type Outcome struct {
	TaskID   string `json:"task_id"`
	Expected string `json:"expected"`
	Picked   string `json:"picked"`
	Served   string `json:"served"` // the tool actually used after overlay routing
	Denied   bool   `json:"denied,omitempty"`
	Correct  bool   `json:"correct"`
	Err      string `json:"err,omitempty"`
	// Unstable marks a task the selector answered inconsistently across re-votes; it is excluded
	// from fixed/broken because it is not evidence about the overlay.
	Unstable bool `json:"unstable,omitempty"`
	// ArgsChecked is true when the selector returned arguments and they were validated against the
	// input schema of the tool actually called. ArgsError says why they were invalid.
	ArgsChecked bool           `json:"args_checked,omitempty"`
	ArgsError   string         `json:"args_error,omitempty"`
	Args        map[string]any `json:"args,omitempty"`
}

// Run is one pass of a selector over a task list under one overlay.
type Run struct {
	Outcomes []Outcome
}

// EvalOptions tunes Evaluate.
type EvalOptions struct {
	// Concurrency bounds parallel selector calls (default 8). Order of results is unaffected.
	Concurrency int
}

// Evaluate asks sel to pick a tool for every task, given the tools the overlay exposes, then
// applies the overlay's routing to the pick. A selector error counts as incorrect (and is recorded),
// so a flaky selector cannot look like an improvement.
func Evaluate(ctx context.Context, sel Selector, tools []Tool, tasks []Task, overlay improvementsandbox.Overlay, opt EvalOptions) (Run, error) {
	if sel == nil {
		return Run{}, fmt.Errorf("selector is nil")
	}
	workers := opt.Concurrency
	if workers <= 0 {
		workers = 8
	}
	schemas := compileSchemas(tools)
	out := make([]Outcome, len(tasks))
	jobs := make(chan int)
	// A fatal selector error (no credits, bad key) stops the run: every further call would fail
	// the same way, and a run made of failures is not evidence about anything.
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	var fatalOnce sync.Once
	var fatalErr error
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				o, ferr := evaluateOne(runCtx, sel, tools, schemas, tasks[i], overlay)
				out[i] = o
				if ferr != nil {
					fatalOnce.Do(func() { fatalErr = ferr; stop() })
				}
			}
		}()
	}
feed:
	for i := range tasks {
		select {
		case jobs <- i:
		case <-runCtx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	if fatalErr != nil {
		return Run{}, fmt.Errorf("selector cannot make calls, run stopped: %w", fatalErr)
	}
	if err := ctx.Err(); err != nil {
		return Run{}, err
	}
	return Run{Outcomes: out}, nil
}

// isFatal reports whether err says no further call can succeed (see agent's fatalAPIError).
func isFatal(err error) bool {
	var f interface{ Fatal() bool }
	return errors.As(err, &f) && f.Fatal()
}

func evaluateOne(ctx context.Context, sel Selector, tools []Tool, schemas map[string]*compiledSchema, t Task, overlay improvementsandbox.Overlay) (Outcome, error) {
	o := Outcome{TaskID: t.ID, Expected: t.ExpectedTool}
	var call Call
	var err error
	cs, withArgs := sel.(CallSelector)
	if withArgs {
		call, err = cs.SelectCall(ctx, t.Query, tools)
	} else {
		call.Name, err = sel.Select(ctx, t.Query, tools)
	}
	if err != nil {
		o.Err = err.Error()
		if isFatal(err) {
			return o, err
		}
		return o, nil
	}
	o.Picked = call.Name
	if call.Name == "" {
		return o, nil
	}
	served, denied, _ := overlay.ResolveRoute(call.Name)
	o.Served, o.Denied = served, denied
	o.Correct = !denied && t.Accepts(served)
	if withArgs {
		o.Args = call.Args
		if call.ArgsInvalid != "" {
			o.ArgsChecked, o.ArgsError, o.Correct = true, call.ArgsInvalid, false
			return o, nil
		}
		// a route renames the tool, not the arguments: validate against the tool actually called
		if s, ok := schemas[served]; ok {
			o.ArgsChecked = true
			if verr := s.validate(call.Args); verr != nil {
				o.ArgsError = verr.Error()
				o.Correct = false
			}
		}
	}
	return o, nil
}
