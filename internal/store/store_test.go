package store

import (
	"database/sql"
	"testing"
	"time"

	"github.com/seanochang/llm-usage-dashboard/internal/model"
)

func openTmp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir() + "/usage.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestUpsertIsIdempotent(t *testing.T) {
	s := openTmp(t)
	records := []model.Record{
		{ID: "req_1", Tool: model.ToolClaude, TS: time.Unix(1000, 0), Model: "claude-opus-5", OutputTok: 10},
		{ID: "req_2", Tool: model.ToolClaude, TS: time.Unix(1001, 0), Model: "claude-opus-5", OutputTok: 20},
	}
	if n, err := s.UpsertRecords(records); err != nil || n != 2 {
		t.Fatalf("first insert: n=%d err=%v", n, err)
	}
	if n, err := s.UpsertRecords(records); err != nil || n != 0 {
		t.Fatalf("re-insert: n=%d err=%v", n, err)
	}
	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM request`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("row count = %d, want 2", count)
	}
}

func TestArchiveSurvivesCursorDeletion(t *testing.T) {
	s := openTmp(t)
	_, err := s.UpsertRecords([]model.Record{{
		ID: "req_1", Tool: model.ToolClaude, TS: time.Unix(1000, 0), Model: "claude-opus-5",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetCursor("/gone.jsonl", model.ToolClaude, 10, 20, 30); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteCursor("/gone.jsonl"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM request`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("deleting a source cursor must not delete archived requests")
	}
}

func TestCursorRoundTrip(t *testing.T) {
	s := openTmp(t)
	if _, _, _, ok := s.Cursor("/a.jsonl"); ok {
		t.Fatal("unknown path should report ok=false")
	}
	if err := s.SetCursor("/a.jsonl", model.ToolCodex, 100, 200, 300); err != nil {
		t.Fatal(err)
	}
	size, mtime, offset, ok := s.Cursor("/a.jsonl")
	if !ok || size != 100 || mtime != 200 || offset != 300 {
		t.Fatalf("got %d %d %d %v", size, mtime, offset, ok)
	}
}

func TestLimitChangeDetection(t *testing.T) {
	s := openTmp(t)
	base := model.LimitSample{Tool: model.ToolClaude, Kind: model.KindSession,
		Percent: 16, ObservedAt: time.Unix(1000, 0), Provenance: model.ProvLive}
	for i := 0; i < 50; i++ {
		sample := base
		sample.ObservedAt = time.Unix(int64(1000+i), 0)
		inserted, err := s.InsertLimitIfChanged(sample)
		if err != nil {
			t.Fatal(err)
		}
		if (i == 0) != inserted {
			t.Fatalf("sample %d inserted=%v", i, inserted)
		}
	}
	changed := base
	changed.Percent = 17
	changed.ObservedAt = time.Unix(2000, 0)
	if inserted, err := s.InsertLimitIfChanged(changed); err != nil || !inserted {
		t.Fatalf("changed percent: inserted=%v err=%v", inserted, err)
	}
	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM limit_sample`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("limit rows = %d, want 2", count)
	}
	var lastSeen int64
	if err := s.DB().QueryRow(`SELECT last_seen FROM limit_sample WHERE percent=16`).Scan(&lastSeen); err != nil {
		t.Fatal(err)
	}
	if lastSeen != 1049 {
		t.Fatalf("identical observations should refresh last_seen: got %d", lastSeen)
	}
}

func TestLimitProvenanceChangeIsStored(t *testing.T) {
	s := openTmp(t)
	cached := model.LimitSample{Tool: model.ToolClaude, Kind: model.KindSession,
		Percent: 16, ObservedAt: time.Unix(1000, 0), Provenance: model.ProvCached}
	if inserted, err := s.InsertLimitIfChanged(cached); err != nil || !inserted {
		t.Fatalf("cached insert: %v %v", inserted, err)
	}
	live := cached
	live.Provenance = model.ProvLive
	live.ObservedAt = time.Unix(1001, 0)
	if inserted, err := s.InsertLimitIfChanged(live); err != nil || !inserted {
		t.Fatalf("live provenance must replace stale cached state: %v %v", inserted, err)
	}
}

func TestUnpricedTracking(t *testing.T) {
	s := openTmp(t)
	for i := 0; i < 3; i++ {
		if err := s.NoteUnpriced("gpt-5-codex", time.Unix(int64(1000+i), 0)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Unpriced()
	if err != nil {
		t.Fatal(err)
	}
	if got["gpt-5-codex"] != 3 {
		t.Fatalf("count = %d, want 3", got["gpt-5-codex"])
	}
}

func TestOpenMigratesLegacyLimitSamples(t *testing.T) {
	path := t.TempDir() + "/legacy.db"
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
      CREATE TABLE limit_sample (
        tool TEXT NOT NULL, kind TEXT NOT NULL, scope TEXT NOT NULL DEFAULT '',
        percent REAL NOT NULL, resets_at INTEGER, is_active INTEGER DEFAULT 0,
        observed_at INTEGER NOT NULL, provenance TEXT NOT NULL,
        PRIMARY KEY (tool,kind,scope,observed_at)
      );
      INSERT INTO limit_sample(tool,kind,scope,percent,observed_at,provenance)
      VALUES('claude','session','',16,1234,'cached');`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var lastSeen int64
	if err := st.DB().QueryRow(`SELECT last_seen FROM limit_sample`).Scan(&lastSeen); err != nil {
		t.Fatal(err)
	}
	if lastSeen != 1234 {
		t.Fatalf("last_seen = %d, want observed_at 1234", lastSeen)
	}
}
