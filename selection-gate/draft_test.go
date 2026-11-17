package selectiongate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

var draftTools = []Tool{
	{Name: "read_text_file", Description: "Read one file"},
	{Name: "list_directory", Description: "List a folder"},
}

func TestParseDraftDropsUntrustworthyTasks(t *testing.T) {
	raw := `{"tasks":[
	 {"tool":"read_text_file","query":"show me what is in notes.txt"},
	 {"tool":"nope_tool","query":"do a thing"},
	 {"tool":"list_directory","query":"use list_directory on src"},
	 {"tool":"list_directory","query":"Please use list directory here"},
	 {"tool":"list_directory","query":"   "},
	 {"tool":"read_text_file","query":"Show me  what is in NOTES.txt"},
	 {"tool":"list_directory","query":"what files are in src?"}]}`
	kept, dropped, err := ParseDraft(raw, draftTools)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 2 || len(dropped) != 5 {
		t.Fatalf("kept=%d dropped=%d (%v)", len(kept), len(dropped), dropped)
	}
	for _, k := range kept {
		if k.Approved {
			t.Fatal("drafted tasks must never start approved")
		}
	}
	again, _, _ := ParseDraft(raw, draftTools)
	if again[0].ID != kept[0].ID || again[1].ID != kept[1].ID {
		t.Fatal("ids not stable")
	}
}

func TestParseDraftRejectsBadJSONAndEmpty(t *testing.T) {
	if _, _, err := ParseDraft(`{"tasks":[}`, draftTools); err == nil {
		t.Fatal("want parse error")
	}
	if _, _, err := ParseDraft(`{"tasks":[{"tool":"x","query":"y"}]}`, draftTools); err == nil {
		t.Fatal("want error when nothing survives")
	}
}

func TestGenerateDraftUsesLLMAndPropagatesErrors(t *testing.T) {
	var gotUser string
	llm := func(_ context.Context, _, user string) (string, error) {
		gotUser = user
		return `{"tasks":[{"tool":"list_directory","query":"what is in src?"}]}`, nil
	}
	d, _, err := GenerateDraft(context.Background(), llm, draftTools, DraftOptions{Name: "n", PerTool: 2})
	if err != nil || len(d.Tasks) != 1 || d.Name != "n" {
		t.Fatalf("%v %+v", err, d)
	}
	if !strings.Contains(gotUser, "TOOL read_text_file") || !strings.Contains(gotUser, "Write 2 requests") {
		t.Fatalf("prompt: %s", gotUser)
	}
	bad := func(context.Context, string, string) (string, error) { return "", errors.New("boom") }
	if _, _, err := GenerateDraft(context.Background(), bad, draftTools, DraftOptions{}); err == nil {
		t.Fatal("want error")
	}
	if _, _, err := GenerateDraft(context.Background(), llm, nil, DraftOptions{}); err == nil {
		t.Fatal("want error for no tools")
	}
}

func bigDraft(n int, approve bool) *Draft {
	d := &Draft{Name: "d"}
	for i := 0; i < n; i++ {
		q := fmt.Sprintf("request number %d", i)
		d.Tasks = append(d.Tasks, DraftTask{ID: taskID(NormalizeQuery(q)), Query: q, ExpectedTool: "list_directory", Approved: approve})
	}
	return d
}

func TestApproveOnlyUsesApprovedTasks(t *testing.T) {
	if _, err := bigDraft(10, false).Approve(0.5, nil); err == nil {
		t.Fatal("nothing approved must be refused")
	}
	d := bigDraft(40, true)
	d.Tasks[0].Approved = false
	g, err := d.Approve(0.5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Tasks) != 39 {
		t.Fatalf("tasks=%d", len(g.Tasks))
	}
	if len(g.Sealed()) == 0 || len(g.Dev()) == 0 {
		t.Fatal("expected both splits")
	}
}

func TestApproveSplitIsDeterministicAndLeakFree(t *testing.T) {
	a, _ := bigDraft(60, true).Approve(0.5, nil)
	b, _ := bigDraft(60, true).Approve(0.5, nil)
	if a.SealedHash() != b.SealedHash() {
		t.Fatal("split not deterministic")
	}
	d := &Draft{Tasks: []DraftTask{
		{ID: "a", Query: "List  the SRC", ExpectedTool: "t", Approved: true},
		{ID: "b", Query: "list the src", ExpectedTool: "t", Approved: true},
	}}
	for i := 0; i < 30; i++ {
		d.Tasks = append(d.Tasks, DraftTask{ID: fmt.Sprint("x", i), Query: fmt.Sprint("q", i), ExpectedTool: "t", Approved: true})
	}
	if _, err := d.Approve(0.5, nil); err != nil {
		t.Fatalf("near-duplicate queries must not leak across splits: %v", err)
	}
}

func TestApproveRejectsUnknownTool(t *testing.T) {
	if _, err := bigDraft(10, true).Approve(0.5, []string{"read_text_file"}); err == nil {
		t.Fatal("want unknown tool error")
	}
}

func TestApproveCarriesAcceptableToolsAndChecksThem(t *testing.T) {
	d := bigDraft(40, true)
	d.Tasks[0].AcceptableTools = []string{"directory_tree"}
	g, err := d.Approve(0.5, []string{"list_directory", "directory_tree"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, task := range g.Tasks {
		if task.ID == d.Tasks[0].ID && len(task.AcceptableTools) == 1 && task.Accepts("directory_tree") {
			found = true
		}
	}
	if !found {
		t.Fatal("acceptable_tools lost on approve")
	}
	d.Tasks[0].AcceptableTools = []string{"ghost"}
	if _, err := d.Approve(0.5, []string{"list_directory", "directory_tree"}); err == nil {
		t.Fatal("an unknown acceptable tool must be rejected")
	}
}
