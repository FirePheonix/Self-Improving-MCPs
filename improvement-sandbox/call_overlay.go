package improvementsandbox

import (
	"context"
	"fmt"
)

// WrapCallApplies overlay routing/compose around an upstream CallFunc.
// Cache is process-local for the duration of a sandbox run.
func WrapCall(upstream CallFunc, overlay Overlay) CallFunc {
	cache := map[string]struct {
		ok bool
		ms int64
	}{}

	return func(ctx context.Context, tool string, args map[string]any) (bool, int64, error) {
		for _, r := range overlay.Routing {
			if r.FromTool != tool {
				continue
			}
			if r.Deny {
				return false, 0, fmt.Errorf("denied: %s", r.Note)
			}
			if r.ToTool != "" {
				tool = r.ToTool
			}
			break
		}

		if ct, ok := overlay.ComposedTools[tool]; ok {
			var lastOK bool
			var lastMs int64
			for _, step := range ct.Steps {
				a := step.Args
				if a == nil {
					a = map[string]any{}
				}
				ok, ms, err := upstream(ctx, step.Tool, a)
				lastOK, lastMs = ok, ms
				if err != nil || !ok {
					return ok, ms, err
				}
			}
			return lastOK, lastMs, nil
		}

		for _, c := range overlay.Cache {
			if c.ToolName != tool {
				continue
			}
			key := tool // args ignored for coarse sandbox cache
			if hit, ok := cache[key]; ok {
				return hit.ok, hit.ms, nil
			}
			ok, ms, err := upstream(ctx, tool, args)
			if err == nil {
				cache[key] = struct {
					ok bool
					ms int64
				}{ok, ms}
			}
			return ok, ms, err
		}

		return upstream(ctx, tool, args)
	}
}
