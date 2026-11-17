package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sort"
	"time"

	improvementsandbox "self-improving-mcp/improvement-sandbox"
	"self-improving-mcp/store"
)

// VersionSource is where the proxy learns which overlay is active. *store.DB implements it.
type VersionSource interface {
	ActiveVersionID() (string, error)
	GetVersion(id string) (store.Version, error)
}

// DefaultReloadInterval is how often the active-version pointer is checked.
const DefaultReloadInterval = 5 * time.Second

// CheckReload compares the active version in src with what the proxy serves and reloads when
// they differ. It reports whether a reload happened.
//
// A failure to read or parse the new overlay never disturbs what is being served: the proxy keeps
// the last good overlay, records the error for /overlay, and retries on the next check.
func (p *Proxy) CheckReload(src VersionSource) (reloaded bool, err error) {
	activeID, err := src.ActiveVersionID()
	if err != nil {
		return false, p.noteReloadErr(fmt.Errorf("read active version: %w", err))
	}
	_, current := p.snapshot()
	if activeID == current {
		p.stateMu.Lock()
		p.reloadErr = "" // the pointer is readable and matches what is served: nothing is wrong now
		p.stateMu.Unlock()
		return false, nil
	}
	var ov improvementsandbox.Overlay
	if activeID != "" {
		ver, err := src.GetVersion(activeID)
		if err != nil {
			return false, p.noteReloadErr(fmt.Errorf("load version %s: %w", activeID, err))
		}
		ov, err = improvementsandbox.ParseOverlay(ver.OverlayJSON)
		if err != nil {
			return false, p.noteReloadErr(fmt.Errorf("parse overlay %s: %w", activeID, err))
		}
	}
	if err := p.validateAgainstUpstream(ov); err != nil {
		return false, p.noteReloadErr(fmt.Errorf("overlay %s rejected: %w", activeID, err))
	}
	p.Reload(activeID, ov)
	return true, nil
}

// validateAgainstUpstream rejects an overlay that points at tools this upstream does not have.
// Overlay validation alone cannot do this: it never sees the live tool list. A route to a
// missing tool would turn every call to the source tool into an error.
func (p *Proxy) validateAgainstUpstream(ov improvementsandbox.Overlay) error {
	p.stateMu.RLock()
	have := map[string]bool{}
	schemas := map[string]any{}
	for _, t := range p.upstreamTools {
		have[t.Name] = true
		schemas[t.Name] = t.InputSchema
	}
	p.stateMu.RUnlock()
	if len(have) == 0 {
		return nil // nothing to check against
	}
	for _, r := range ov.Routing {
		if !r.Deny && r.ToTool != "" && !have[r.ToTool] && ov.ComposedTools[r.ToTool].Steps == nil {
			return fmt.Errorf("route %s -> %s: target tool does not exist upstream", r.FromTool, r.ToTool)
		}
	}
	hidden := 0
	for name, p := range ov.ToolPatches {
		if !p.Hidden {
			continue
		}
		if !have[name] {
			return fmt.Errorf("overlay hides %s, which the upstream does not have", name)
		}
		hidden++
	}
	if hidden > 0 && hidden >= len(have) {
		return fmt.Errorf("overlay hides every upstream tool")
	}
	// a route renames the tool but not the arguments: refuse routes whose target cannot take them
	if problems := improvementsandbox.CheckRoutes(ov, schemas); len(problems) > 0 {
		return fmt.Errorf("%s", problems[0])
	}
	for _, c := range ov.Cache {
		if !have[c.ToolName] {
			return fmt.Errorf("cache rule for %s: tool does not exist upstream", c.ToolName)
		}
	}
	for name, ct := range ov.ComposedTools {
		if have[name] {
			return fmt.Errorf("composed tool %s shadows an upstream tool", name)
		}
		for _, st := range ct.Steps {
			if !have[st.Tool] {
				return fmt.Errorf("composed tool %s step uses %s: tool does not exist upstream", name, st.Tool)
			}
		}
	}
	return nil
}

func (p *Proxy) noteReloadErr(err error) error {
	p.stateMu.Lock()
	p.reloadErr = err.Error()
	p.stateMu.Unlock()
	return err
}

// WatchActive polls src every interval until ctx ends, hot-reloading on a promote or rollback.
func (p *Proxy) WatchActive(ctx context.Context, src VersionSource, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultReloadInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	lastLogged := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			reloaded, err := p.CheckReload(src)
			switch {
			case err != nil && err.Error() != lastLogged:
				log.Printf("overlay reload failed (still serving previous overlay): %v", err)
				lastLogged = err.Error()
			case reloaded:
				_, v := p.snapshot()
				if v == "" {
					v = "none (baseline)"
				}
				log.Printf("overlay reloaded: active version %s", v)
				lastLogged = ""
			}
		}
	}
}

// Status is what /overlay reports.
type Status struct {
	ActiveVersion string                     `json:"active_version"` // "none" for the baseline
	LoadedAt      time.Time                  `json:"loaded_at"`
	LastReloadErr string                     `json:"last_reload_error,omitempty"`
	Tools         []string                   `json:"tools"`
	Overlay       improvementsandbox.Overlay `json:"overlay"`
}

// Status returns the current serving state.
func (p *Proxy) Status() Status {
	p.stateMu.RLock()
	defer p.stateMu.RUnlock()
	st := Status{ActiveVersion: "none", LastReloadErr: p.reloadErr}
	if p.state != nil {
		st.Overlay = p.state.overlay
		st.LoadedAt = p.state.loadedAt
		if p.state.version != "" {
			st.ActiveVersion = p.state.version
		}
	}
	for n := range p.registered {
		st.Tools = append(st.Tools, n)
	}
	sort.Strings(st.Tools)
	return st
}

// StatusHandler serves GET /health and GET /overlay. Both are read-only.
//
// /health answers 200 only when the proxy has an upstream and has registered tools, so an
// orchestrator can restart a proxy whose upstream never came up. /overlay shows the overlay in
// force, including descriptions agents are reading, so reviewers can see what is live.
func (p *Proxy) StatusHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		st := p.Status()
		ok := p.Upstream != nil && len(st.Tools) > 0
		code := http.StatusOK
		if !ok {
			code = http.StatusServiceUnavailable
		}
		writeJSON(w, code, map[string]any{
			"ok":             ok,
			"active_version": st.ActiveVersion,
			"tools":          len(st.Tools),
			"reload_error":   st.LastReloadErr,
		})
	})
	mux.HandleFunc("/overlay", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, p.Status())
	})
	return mux
}

// ServeStatus listens on addr until ctx ends. Use a loopback address unless auth fronts it.
func (p *Proxy) ServeStatus(ctx context.Context, addr string) (net.Addr, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("status listen %s: %w", addr, err)
	}
	srv := &http.Server{Handler: p.StatusHandler(), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	return ln.Addr(), nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
