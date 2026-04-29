package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestInsertEventDedupesByHash(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.UpsertTool(ctx, Tool{Name: "codex", DisplayName: "Codex", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	ev := Event{
		EventHash:    "same",
		ToolName:     "codex",
		SourcePath:   "/tmp/log.jsonl",
		SourceLine:   1,
		OccurredAt:   time.Now().UTC(),
		InputTokens:  10,
		OutputTokens: 5,
		TotalTokens:  15,
		RawKind:      "json",
	}
	inserted, err := db.InsertEvent(ctx, ev)
	if err != nil {
		t.Fatal(err)
	}
	if !inserted {
		t.Fatal("first insert should insert")
	}
	inserted, err = db.InsertEvent(ctx, ev)
	if err != nil {
		t.Fatal(err)
	}
	if inserted {
		t.Fatal("second insert should be ignored")
	}
	summary, err := db.BuildSummary(ctx, 30)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Totals.Events != 1 || summary.Totals.TotalTokens != 15 {
		t.Fatalf("unexpected summary: %+v", summary.Totals)
	}
}

func TestFileStateResetsWhenFileShrinks(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	state := FileState{
		SourcePath: "/tmp/session.jsonl",
		SizeBytes:  100,
		ModTime:    time.Now().UTC(),
		Offset:     100,
		LineCount:  10,
	}
	if err := db.SaveFileState(ctx, state); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetFileState(ctx, state.SourcePath, 50)
	if err != nil {
		t.Fatal(err)
	}
	if got.Offset != 0 || got.LineCount != 0 {
		t.Fatalf("expected reset state after shrink, got %+v", got)
	}
}
