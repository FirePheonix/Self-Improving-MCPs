package selectiongate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func task(id, q, tool string, s Split) Task {
	return Task{ID: id, Query: q, ExpectedTool: tool, Split: s}
}

func TestValidateAcceptsGoodSet(t *testing.T) {
	g := &GoldenSet{Tasks: []Task{
		task("a", "list files", "list_directory", SplitDev),
		task("b", "read these two files", "read_multiple_files", SplitSealed),
	}}
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(g.Dev()) != 1 || len(g.Sealed()) != 1 {
		t.Fatalf("split counts wrong: dev=%d sealed=%d", len(g.Dev()), len(g.Sealed()))
	}
}

func TestValidateRejectsBadSets(t *testing.T) {
	cases := map[string]struct {
		tasks []Task
		want  string
	}{
		"empty":        {nil, "no tasks"},
		"empty id":     {[]Task{task("", "q", "t", SplitSealed)}, "empty id"},
		"duplicate id": {[]Task{task("a", "q1", "t", SplitDev), task("a", "q2", "t", SplitSealed)}, "duplicate id"},
		"empty query":  {[]Task{task("a", "  ", "t", SplitSealed)}, "empty query"},
		"empty tool":   {[]Task{task("a", "q", "", SplitSealed)}, "empty expected_tool"},
		"bad split":    {[]Task{task("a", "q", "t", Split("train"))}, "split must be"},
		"no sealed":    {[]Task{task("a", "q", "t", SplitDev)}, "no sealed tasks"},
		"leak same q":  {[]Task{task("a", "Read  the FILE", "t", SplitDev), task("b", "read the file", "t", SplitSealed)}, "leakage"},
	}
	for name, c := range cases {
		err := (&GoldenSet{Tasks: c.tasks}).Validate()
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want containing %q", name, err, c.want)
		}
	}
}

func TestSameQueryWithinOneSplitIsAllowed(t *testing.T) {
	g := &GoldenSet{Tasks: []Task{
		task("a", "same", "t", SplitSealed),
		task("b", "same", "t", SplitSealed),
	}}
	if err := g.Validate(); err != nil {
		t.Fatalf("duplicates inside one split are not leakage: %v", err)
	}
}

func TestSealedHashIsOrderIndependentAndLabelSensitive(t *testing.T) {
	a := task("a", "q1", "t1", SplitSealed)
	b := task("b", "q2", "t2", SplitSealed)
	dev := task("d", "q3", "t3", SplitDev)

	h1 := (&GoldenSet{Tasks: []Task{a, b, dev}}).SealedHash()
	h2 := (&GoldenSet{Tasks: []Task{dev, b, a}}).SealedHash()
	if h1 != h2 {
		t.Error("hash depends on task order")
	}
	// dev edits must not change the sealed fingerprint
	dev2 := task("d", "other", "t9", SplitDev)
	if h3 := (&GoldenSet{Tasks: []Task{a, b, dev2}}).SealedHash(); h3 != h1 {
		t.Error("dev change altered the sealed hash")
	}
	// relabeling a sealed task must change it
	b2 := task("b", "q2", "WRONG", SplitSealed)
	if h4 := (&GoldenSet{Tasks: []Task{a, b2, dev}}).SealedHash(); h4 == h1 {
		t.Error("sealed relabel did not change the hash")
	}
}

func TestLoadGoldenRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "g.json")
	body := `{"name":"t","tasks":[{"id":"a","query":"q","expected_tool":"x","split":"sealed"}]}`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	g, err := LoadGolden(p)
	if err != nil || len(g.Sealed()) != 1 {
		t.Fatalf("LoadGolden: %v %+v", err, g)
	}
	if _, err := LoadGolden(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("expected error for missing file")
	}
	bad := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(bad, []byte(`{"tasks":[{"id":"a","query":"q","expected_tool":"x","split":"nope"}]}`), 0o600)
	if _, err := LoadGolden(bad); err == nil {
		t.Error("expected validation error")
	}
}
