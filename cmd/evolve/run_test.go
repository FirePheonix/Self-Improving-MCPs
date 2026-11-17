package main

import (
	"path/filepath"
	"testing"
	"time"

	"self-improving-mcp/store"
)

func TestRunNeverJudgesACandidateFromAnEarlierRun(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	old, _ := db.InsertVersion("", `{}`, "from yesterday")
	if _, err := db.SQL().Exec(`UPDATE versions SET created_at = ? WHERE id = ?`, "2020-01-01T00:00:00Z", old.ID); err != nil {
		t.Fatal(err)
	}
	since := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	if got, err := newestCandidateSince(db, since); err != nil || got != "" {
		t.Fatalf("a stale candidate was picked up: %q %v", got, err)
	}
	fresh, _ := db.InsertVersion("", `{}`, "this run")
	if got, _ := newestCandidateSince(db, since); got != fresh.ID {
		t.Fatalf("got %q, want this run's candidate %q", got, fresh.ID)
	}
	if _, err := db.SQL().Exec(`UPDATE versions SET status = 'discarded' WHERE id = ?`, fresh.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := newestCandidateSince(db, since); got != "" {
		t.Fatalf("a discarded version is not a candidate: %q", got)
	}
}
