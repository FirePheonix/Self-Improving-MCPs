package store

import (
	"path/filepath"
	"testing"
)

func TestMigrateAndRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	session, err := db.CreateSession("fixture-upstream")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	gotSession, err := db.GetSession(session.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if gotSession.Upstream != "fixture-upstream" {
		t.Fatalf("Upstream = %q", gotSession.Upstream)
	}

	_, err = db.InsertToolCall(ToolCall{
		SessionID:     session.ID,
		Seq:           1,
		ToolName:      "echo",
		ArgsJSON:      `{"text":"hi"}`,
		ArgsHash:      "abc",
		ResultSummary: "hi",
		LatencyMs:     12,
	})
	if err != nil {
		t.Fatalf("InsertToolCall: %v", err)
	}
	calls, err := db.ListToolCallsBySession(session.ID)
	if err != nil {
		t.Fatalf("ListToolCallsBySession: %v", err)
	}
	if len(calls) != 1 || calls[0].ToolName != "echo" {
		t.Fatalf("calls = %+v", calls)
	}

	version, err := db.InsertVersion("", `{"tools":{}}`, "baseline")
	if err != nil {
		t.Fatalf("InsertVersion: %v", err)
	}
	if err := db.SetActiveVersion(version.ID); err != nil {
		t.Fatalf("SetActiveVersion: %v", err)
	}
	active, err := db.ActiveVersionID()
	if err != nil {
		t.Fatalf("ActiveVersionID: %v", err)
	}
	if active != version.ID {
		t.Fatalf("active = %q, want %q", active, version.ID)
	}

	finding, err := db.InsertFinding(Finding{
		Category: "redundant",
		Severity: "warn",
		Title:    "duplicate echo",
	})
	if err != nil {
		t.Fatalf("InsertFinding: %v", err)
	}
	findings, err := db.ListFindings()
	if err != nil {
		t.Fatalf("ListFindings: %v", err)
	}
	if len(findings) != 1 || findings[0].ID != finding.ID {
		t.Fatalf("findings = %+v", findings)
	}

	exp, err := db.InsertExperiment(version.ID, "")
	if err != nil {
		t.Fatalf("InsertExperiment: %v", err)
	}
	if err := db.UpdateExperimentResult(exp.ID, "passed", `{"ok":true}`); err != nil {
		t.Fatalf("UpdateExperimentResult: %v", err)
	}
	gotExp, err := db.GetExperiment(exp.ID)
	if err != nil {
		t.Fatalf("GetExperiment: %v", err)
	}
	if gotExp.Status != "passed" {
		t.Fatalf("status = %q", gotExp.Status)
	}
}

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

func TestActiveVersionLifecycle(t *testing.T) {
	db := openTestDB(t)

	if id, err := db.ActiveVersionID(); err != nil || id != "" {
		t.Fatalf("fresh store: ActiveVersionID = %q, %v; want empty, nil", id, err)
	}

	v, err := db.InsertVersion("", `{"routing":[]}`, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetActiveVersion(v.ID); err != nil {
		t.Fatalf("SetActiveVersion: %v", err)
	}
	if id, _ := db.ActiveVersionID(); id != v.ID {
		t.Fatalf("active = %q, want %q", id, v.ID)
	}
}

func TestSetActiveVersionRejectsUnknownVersion(t *testing.T) {
	db := openTestDB(t)
	if err := db.SetActiveVersion("does-not-exist"); err == nil {
		t.Fatal("promoting a version that does not exist must fail")
	}
	if id, _ := db.ActiveVersionID(); id != "" {
		t.Fatalf("failed promote changed the active pointer to %q", id)
	}
}

func TestActivePointerSurvivesMissingRow(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.SQL().Exec(`DELETE FROM active_version`); err != nil {
		t.Fatal(err)
	}
	if id, err := db.ActiveVersionID(); err != nil || id != "" {
		t.Fatalf("missing pointer row: ActiveVersionID = %q, %v; want empty, nil", id, err)
	}
	v, _ := db.InsertVersion("", `{}`, "t")
	if err := db.SetActiveVersion(v.ID); err != nil {
		t.Fatalf("SetActiveVersion with missing row: %v", err)
	}
	if id, _ := db.ActiveVersionID(); id != v.ID {
		t.Fatalf("promote was silently lost: active = %q, want %q", id, v.ID)
	}
}

func TestClearActiveVersionReturnsToBaseline(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	v, err := db.InsertVersion("", `{}`, "n")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetActiveVersion(v.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.ActiveVersionID(); got != v.ID {
		t.Fatalf("active = %q", got)
	}
	if err := db.ClearActiveVersion(); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.ActiveVersionID(); got != "" {
		t.Fatalf("active after clear = %q, want baseline", got)
	}
	// idempotent, and the version row survives for a later re-promote
	if err := db.ClearActiveVersion(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetVersion(v.ID); err != nil {
		t.Fatal(err)
	}
}

func TestExperimentsForVersionNewestFirst(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	v, _ := db.InsertVersion("", `{}`, "n")
	other, _ := db.InsertVersion("", `{}`, "o")
	e1, _ := db.InsertExperiment(v.ID, "")
	e2, _ := db.InsertExperiment(v.ID, "")
	_, _ = db.InsertExperiment(other.ID, "")
	_ = db.UpdateExperimentResult(e1.ID, "failed", `{"a":1}`)
	_ = db.UpdateExperimentResult(e2.ID, "passed", `{"a":2}`)
	got, err := db.ExperimentsForVersion(v.ID)
	if err != nil || len(got) != 2 || got[0].ID != e2.ID || got[0].Status != "passed" || got[1].Status != "failed" {
		t.Fatalf("%v %+v", err, got)
	}
	if none, _ := db.ExperimentsForVersion("nope"); len(none) != 0 {
		t.Fatal("unknown version must return nothing")
	}
}
