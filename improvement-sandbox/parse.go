package improvementsandbox

import (
	"encoding/json"
	"fmt"
)

// ParseOverlay unmarshals overlay JSON. Empty/{} returns an empty overlay.
func ParseOverlay(raw string) (Overlay, error) {
	if raw == "" || raw == "{}" {
		return Overlay{}, nil
	}
	var o Overlay
	if err := json.Unmarshal([]byte(raw), &o); err != nil {
		return Overlay{}, fmt.Errorf("parse overlay: %w", err)
	}
	if len(o.ToolPatches) == 0 && len(o.Routing) == 0 && len(o.Cache) == 0 && len(o.ComposedTools) == 0 {
		return Overlay{}, nil
	}
	if err := Validate(o); err != nil {
		return Overlay{}, err
	}
	return o, nil
}
