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
