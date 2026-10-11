package tracer

import (
	"context"
	"path/filepath"
	"testing"

	"self-improving-mcp/store"
)

func TestSessionTracerAssignsSequence(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}

	tr, err := StartSession(db, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := tr.RecordCall(ctx, Call{ToolName: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := tr.RecordCall(ctx, Call{ToolName: "b"}); err != nil {
		t.Fatal(err)
	}

	calls, err := db.ListToolCallsBySession(tr.SessionID())
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0].Seq != 1 || calls[1].Seq != 2 {
		t.Fatalf("calls = %+v", calls)
	}
}
