package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"self-improving-mcp/store"
)

type fakeUpstream struct {
	mu    sync.Mutex
	calls []string
	n     atomic.Int64
}

func (f *fakeUpstream) ListTools(context.Context) ([]ToolInfo, error) {
	schema := map[string]any{"type": "object", "properties": map[string]any{}}
	return []ToolInfo{
		{Name: "read_text_file", Description: "Read a file", InputSchema: schema},
		{Name: "read_multiple_files", Description: "Read many files", InputSchema: schema},
	}, nil
}

func (f *fakeUpstream) CallTool(_ context.Context, name string, _ map[string]any) (CallResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, name)
	f.mu.Unlock()
	return CallResult{Content: []Content{{Type: "text", Text: name + "#" + string(rune('0'+f.n.Add(1)%10))}}}, nil
}
func (f *fakeUpstream) Close() error { return nil }
func (f *fakeUpstream) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// fakeSource is an in-memory VersionSource.
type fakeSource struct {
	mu       sync.Mutex
	active   string
	versions map[string]string
	err      error
}

func (s *fakeSource) set(id string) { s.mu.Lock(); s.active = id; s.mu.Unlock() }
func (s *fakeSource) ActiveVersionID() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active, s.err
}
func (s *fakeSource) GetVersion(id string) (store.Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, ok := s.versions[id]
	if !ok {
		return store.Version{}, errors.New("no such version")
	}
	return store.Version{ID: id, OverlayJSON: raw}, nil
}

type harness struct {
	p       *Proxy
	up      *fakeUpstream
	session *mcpsdk.ClientSession
	changed atomic.Int64
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{up: &fakeUpstream{}}
	h.p = NewProxy(h.up)
	if err := h.p.RegisterUpstreamTools(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ct, st := mcpsdk.NewInMemoryTransports()
	if _, err := h.p.Server.SDK().Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "c", Version: "1"}, &mcpsdk.ClientOptions{
		ToolListChangedHandler: func(context.Context, *mcpsdk.ToolListChangedRequest) { h.changed.Add(1) },
	})
	sess, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	h.session = sess
	return h
}

func (h *harness) tools(t *testing.T) map[string]string {
	t.Helper()
	res, err := h.session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, tool := range res.Tools {
		out[tool.Name] = tool.Description
	}
	return out
}

func (h *harness) call(t *testing.T, name string) string {
	t.Helper()
	res, err := h.session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: name, Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) == 0 {
		return ""
	}
	return res.Content[0].(*mcpsdk.TextContent).Text
}

const overlayV1 = `{
 "tool_patches":{"read_text_file":{"description":"Read ONE file only. For several files use read_multiple_files."}},
 "routing":[{"from_tool":"read_text_file","to_tool":"read_multiple_files"}],
 "composed_tools":{"read_both":{"description":"Read both","steps":[{"tool":"read_text_file"}]}}
}`

func TestReloadSwapsDescriptionsRoutingAndComposedToolsLive(t *testing.T) {
	h := newHarness(t)
	src := &fakeSource{versions: map[string]string{"v1": overlayV1}}

	before := h.tools(t)
	if before["read_text_file"] != "Read a file" || len(before) != 2 {
		t.Fatalf("baseline tools: %v", before)
	}
	h.call(t, "read_text_file")
	if got := h.up.called(); got[len(got)-1] != "read_text_file" {
		t.Fatalf("baseline must not route: %v", got)
	}

	src.set("v1")
	if ok, err := h.p.CheckReload(src); !ok || err != nil {
		t.Fatalf("reload: %v %v", ok, err)
	}
	after := h.tools(t)
	if !strings.HasPrefix(after["read_text_file"], "Read ONE file") {
		t.Fatalf("description not patched live: %v", after)
	}
	if _, ok := after["read_both"]; !ok {
		t.Fatalf("composed tool missing: %v", after)
	}
	h.call(t, "read_text_file")
	if got := h.up.called(); got[len(got)-1] != "read_multiple_files" {
		t.Fatalf("routing not applied after reload: %v", got)
	}
	time.Sleep(50 * time.Millisecond)
	if h.changed.Load() == 0 {
		t.Fatal("client was never told the tool list changed")
	}

	// no further change: no reload
	if ok, _ := h.p.CheckReload(src); ok {
		t.Fatal("reloaded without a change")
	}

	// rollback to baseline removes the composed tool and restores the description
	src.set("")
	if ok, err := h.p.CheckReload(src); !ok || err != nil {
		t.Fatalf("rollback: %v %v", ok, err)
	}
	back := h.tools(t)
	if back["read_text_file"] != "Read a file" || len(back) != 2 {
		t.Fatalf("baseline not restored: %v", back)
	}
	h.call(t, "read_text_file")
	if got := h.up.called(); got[len(got)-1] != "read_text_file" {
		t.Fatalf("routing survived rollback: %v", got)
	}
}

func TestBadOverlayNeverReplacesGoodOne(t *testing.T) {
	h := newHarness(t)
	src := &fakeSource{versions: map[string]string{
		"good": overlayV1,
		"bad":  `{"routing":[{"from_tool":"read_text_file","to_tool":"no_such_tool"}]}`,
		"junk": `{not json`,
	}}
	src.set("good")
	if ok, _ := h.p.CheckReload(src); !ok {
		t.Fatal("good overlay should load")
	}
	for _, id := range []string{"bad", "junk", "missing"} {
		src.set(id)
		ok, err := h.p.CheckReload(src)
		if ok || err == nil {
			t.Fatalf("%s: reloaded=%v err=%v; must refuse", id, ok, err)
		}
		if st := h.p.Status(); st.ActiveVersion != "good" || st.LastReloadErr == "" {
			t.Fatalf("%s: status %+v", id, st)
		}
	}
	// the good overlay is still serving
	if !strings.HasPrefix(h.tools(t)["read_text_file"], "Read ONE file") {
		t.Fatal("good overlay stopped serving")
	}
	// a store outage is also non-destructive
	src.err = errors.New("db down")
	if ok, err := h.p.CheckReload(src); ok || err == nil {
		t.Fatal("store error must not reload")
	}
	src.err = nil
	src.set("good")
	if _, err := h.p.CheckReload(src); err != nil {
		t.Fatal(err)
	}
	if st := h.p.Status(); st.LastReloadErr != "" {
		t.Fatalf("error should clear after recovery: %q", st.LastReloadErr)
	}
}

func TestReloadClearsResultCache(t *testing.T) {
	h := newHarness(t)
	cached := `{"cache":[{"tool_name":"read_text_file","ttl_seconds":600}]}`
	src := &fakeSource{versions: map[string]string{"c": cached, "plain": `{"tool_patches":{"read_text_file":{"description":"x"}}}`}}
	src.set("c")
	h.p.CheckReload(src)
	a := h.call(t, "read_text_file")
	b := h.call(t, "read_text_file")
	if a != b {
		t.Fatalf("cache not used: %q vs %q", a, b)
	}
	src.set("plain")
	h.p.CheckReload(src)
	if c := h.call(t, "read_text_file"); c == a {
		t.Fatal("stale cached result served after the overlay changed")
	}
}

// Run with -race: calls and reloads interleave.
func TestConcurrentCallsAndReloads(t *testing.T) {
	h := newHarness(t)
	src := &fakeSource{versions: map[string]string{"v1": overlayV1}}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					if _, err := h.session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "read_multiple_files", Arguments: map[string]any{}}); err != nil {
						t.Error(err)
						return
					}
				}
			}
		}()
	}
	for i := 0; i < 40; i++ {
		if i%2 == 0 {
			src.set("v1")
		} else {
			src.set("")
		}
		if _, err := h.p.CheckReload(src); err != nil {
			t.Error(err)
		}
		_ = h.p.Status()
	}
	close(stop)
	wg.Wait()
}

func TestWatchActiveReloadsAndStopsWithContext(t *testing.T) {
	h := newHarness(t)
	src := &fakeSource{versions: map[string]string{"v1": overlayV1}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { h.p.WatchActive(ctx, src, 10*time.Millisecond); close(done) }()
	src.set("v1")
	deadline := time.Now().Add(2 * time.Second)
	for h.p.Status().ActiveVersion != "v1" {
		if time.Now().After(deadline) {
			t.Fatal("watcher never reloaded")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not stop")
	}
}

func TestStatusEndpoints(t *testing.T) {
	h := newHarness(t)
	srv := httptest.NewServer(h.p.StatusHandler())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/health")
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("health: %v %v", err, res)
	}
	var health map[string]any
	_ = json.NewDecoder(res.Body).Decode(&health)
	if health["ok"] != true || health["active_version"] != "none" {
		t.Fatalf("health body: %v", health)
	}

	src := &fakeSource{versions: map[string]string{"v1": overlayV1}}
	src.set("v1")
	h.p.CheckReload(src)
	res, _ = http.Get(srv.URL + "/overlay")
	var st Status
	_ = json.NewDecoder(res.Body).Decode(&st)
	if st.ActiveVersion != "v1" || len(st.Tools) != 3 || len(st.Overlay.Routing) != 1 {
		t.Fatalf("overlay body: %+v", st)
	}
	if r, _ := http.Post(srv.URL+"/overlay", "text/plain", nil); r.StatusCode == 200 {
		// read-only is enforced by the method being ignored for state; make sure it changed nothing
		if h.p.Status().ActiveVersion != "v1" {
			t.Fatal("POST changed state")
		}
	}

	// A proxy with no tools is unhealthy.
	empty := NewProxy(&fakeUpstream{})
	es := httptest.NewServer(empty.StatusHandler())
	defer es.Close()
	if r, _ := http.Get(es.URL + "/health"); r.StatusCode != 503 {
		t.Fatalf("empty proxy health = %d, want 503", r.StatusCode)
	}
}

func TestRouteWhoseTargetCannotTakeTheArgumentsIsRefused(t *testing.T) {
	p := NewProxy(&argUpstream{})
	if err := p.RegisterUpstreamTools(context.Background()); err != nil {
		t.Fatal(err)
	}
	bad := `{"routing":[{"from_tool":"read_text_file","to_tool":"read_multiple_files"}]}`
	alias := `{"routing":[{"from_tool":"read_file","to_tool":"read_text_file"}]}`
	src := &fakeSource{versions: map[string]string{"bad": bad, "alias": alias}}

	src.set("bad")
	ok, err := p.CheckReload(src)
	if ok || err == nil || !strings.Contains(err.Error(), "cannot work") || !strings.Contains(err.Error(), `requires "paths"`) {
		t.Fatalf("a route that would break every routed call must be refused: ok=%v err=%v", ok, err)
	}
	if st := p.Status(); st.ActiveVersion != "none" || st.LastReloadErr == "" {
		t.Fatalf("status: %+v", st)
	}
	src.set("alias")
	if ok, err := p.CheckReload(src); !ok || err != nil {
		t.Fatalf("a genuine alias must load: ok=%v err=%v", ok, err)
	}
}

// argUpstream has tools with real, differing argument schemas.
type argUpstream struct{ fakeUpstream }

func (a *argUpstream) ListTools(context.Context) ([]ToolInfo, error) {
	one := map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []any{"path"}}
	many := map[string]any{"type": "object", "properties": map[string]any{"paths": map[string]any{"type": "array"}}, "required": []any{"paths"}}
	return []ToolInfo{
		{Name: "read_file", Description: "Read a file (deprecated alias)", InputSchema: one},
		{Name: "read_text_file", Description: "Read a file as text", InputSchema: one},
		{Name: "read_multiple_files", Description: "Read many files", InputSchema: many},
	}, nil
}

func TestHiddenToolIsNotOfferedAndUnsafeHidingIsRefused(t *testing.T) {
	h := newHarness(t)
	src := &fakeSource{versions: map[string]string{
		"hide":    `{"tool_patches":{"read_text_file":{"hidden":true}}}`,
		"unknown": `{"tool_patches":{"nope":{"hidden":true}}}`,
		"all":     `{"tool_patches":{"read_text_file":{"hidden":true},"read_multiple_files":{"hidden":true}}}`,
	}}
	src.set("hide")
	if ok, err := h.p.CheckReload(src); !ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
	got := h.tools(t)
	if _, still := got["read_text_file"]; still || len(got) != 1 {
		t.Fatalf("hidden tool still offered: %v", got)
	}
	for _, id := range []string{"unknown", "all"} {
		src.set(id)
		if ok, err := h.p.CheckReload(src); ok || err == nil {
			t.Fatalf("%s must be refused", id)
		}
	}
	if len(h.tools(t)) != 1 {
		t.Fatal("a refused overlay changed what is served")
	}
}
