package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/seanochang/llm-usage-dashboard/internal/model"
	_ "modernc.org/sqlite"
)

type Store struct {
	db   *sql.DB
	path string
}

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("database path is empty")
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}
	db.SetMaxOpenConns(8)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("initialize database %s: %w", path, err)
		}
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize database %s: %w", path, err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate database %s: %w", path, err)
	}
	return &Store{db: db, path: path}, nil
}

func migrate(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(limit_sample)`)
	if err != nil {
		return err
	}
	hasLastSeen := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		if name == "last_seen" {
			hasLastSeen = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !hasLastSeen {
		if _, err := db.Exec(`ALTER TABLE limit_sample ADD COLUMN last_seen INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
		if _, err := db.Exec(`UPDATE limit_sample SET last_seen=observed_at WHERE last_seen=0`); err != nil {
			return err
		}
	}
	_, err = db.Exec(`INSERT INTO meta(key,value) VALUES('schema_version','2')
      ON CONFLICT(key) DO UPDATE SET value=excluded.value`)
	return err
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) DB() *sql.DB { return s.db }

// UpsertRecords inserts records whose natural IDs have not already been seen.
func (s *Store) UpsertRecords(records []model.Record) (int, error) {
	inserted, err := s.UpsertRecordsDetailed(records)
	return len(inserted), err
}

// UpsertRecordsDetailed also returns the records actually inserted. Ingestion
// uses this to avoid incrementing unpriced counts during a --full reparse.
func (s *Store) UpsertRecordsDetailed(records []model.Record) ([]model.Record, error) {
	if len(records) == 0 {
		return nil, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO request
      (id,tool,ts,model,project,session,agent,workflow,depth,
       in_tok,out_tok,think_tok,cache_read,cache_w5m,cache_w1h,anomaly)
      VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	inserted := make([]model.Record, 0, len(records))
	for _, r := range records {
		result, err := stmt.Exec(
			r.ID, string(r.Tool), r.TS.Unix(), r.Model, r.Project, r.Session,
			r.Agent, r.Workflow, r.Depth, r.InputTok, r.OutputTok,
			r.ThinkingTok, r.CacheReadTok, r.CacheWrite5m, r.CacheWrite1h,
			boolInt(r.Anomaly),
		)
		if err != nil {
			return nil, err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if n == 1 {
			inserted = append(inserted, r)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return inserted, nil
}

func (s *Store) Cursor(path string) (size, mtime, offset int64, ok bool) {
	err := s.db.QueryRow(
		`SELECT size, mtime, offset FROM source_file WHERE path = ?`, path,
	).Scan(&size, &mtime, &offset)
	return size, mtime, offset, err == nil
}

func (s *Store) SetCursor(path string, tool model.Tool, size, mtime, offset int64) error {
	_, err := s.db.Exec(`INSERT INTO source_file(path,tool,size,mtime,offset,last_seen)
      VALUES(?,?,?,?,?,?)
      ON CONFLICT(path) DO UPDATE SET
        tool=excluded.tool, size=excluded.size, mtime=excluded.mtime,
        offset=excluded.offset, last_seen=excluded.last_seen`,
		path, string(tool), size, mtime, offset, time.Now().Unix())
	return err
}

// DeleteCursor forgets only ingest state. It never removes archived requests.
func (s *Store) DeleteCursor(path string) error {
	_, err := s.db.Exec(`DELETE FROM source_file WHERE path = ?`, path)
	return err
}

func (s *Store) NoteUnpriced(modelID string, at time.Time) error {
	_, err := s.db.Exec(`INSERT INTO unpriced(model,count,first_seen,last_seen)
      VALUES(?,1,?,?)
      ON CONFLICT(model) DO UPDATE SET
        count=unpriced.count+1, last_seen=excluded.last_seen`,
		modelID, at.Unix(), at.Unix())
	return err
}

func (s *Store) Unpriced() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT model, count FROM unpriced ORDER BY count DESC, model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]int)
	for rows.Next() {
		var name string
		var count int
		if err := rows.Scan(&name, &count); err != nil {
			return nil, err
		}
		out[name] = count
	}
	return out, rows.Err()
}

// ReconcileUnpriced rebuilds the derived table from the durable archive and
// current pricing. This makes editing pricing.toml take effect without a full
// re-ingest and removes models once the user supplies a verified rate.
func (s *Store) ReconcileUnpriced(pricing *model.Pricing) error {
	rows, err := s.db.Query(`SELECT model,COUNT(*),MIN(ts),MAX(ts) FROM request GROUP BY model`)
	if err != nil {
		return err
	}
	type summary struct {
		count       int
		first, last int64
	}
	unpriced := make(map[string]summary)
	for rows.Next() {
		var name string
		var current summary
		if err := rows.Scan(&name, &current.count, &current.first, &current.last); err != nil {
			rows.Close()
			return err
		}
		name = model.NormalizeModel(name)
		if pricing.HasRate(name) {
			continue
		}
		combined := unpriced[name]
		combined.count += current.count
		if combined.first == 0 || current.first < combined.first {
			combined.first = current.first
		}
		if current.last > combined.last {
			combined.last = current.last
		}
		unpriced[name] = combined
	}
	if err := rows.Close(); err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM unpriced`); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO unpriced(model,count,first_seen,last_seen) VALUES(?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for name, current := range unpriced {
		if _, err := stmt.Exec(name, current.count, current.first, current.last); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// InsertLimitIfChanged stores transition points. Repeated identical live
// observations only refresh last_seen so the UI's age remains truthful.
func (s *Store) InsertLimitIfChanged(sample model.LimitSample) (bool, error) {
	var (
		previousPercent float64
		previousReset   sql.NullInt64
		previousActive  int
		previousProv    string
		previousAt      int64
	)
	err := s.db.QueryRow(`SELECT percent,resets_at,is_active,provenance,observed_at
      FROM limit_sample WHERE tool=? AND kind=? AND scope=?
      ORDER BY observed_at DESC LIMIT 1`,
		string(sample.Tool), string(sample.Kind), sample.Scope,
	).Scan(&previousPercent, &previousReset, &previousActive, &previousProv, &previousAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	reset := nullableUnix(sample.ResetsAt)
	active := boolInt(sample.IsActive)
	observed := sample.ObservedAt.Unix()
	if sample.ObservedAt.IsZero() {
		observed = time.Now().Unix()
	}
	if err == nil && previousPercent == sample.Percent &&
		nullIntEqual(previousReset, reset) && previousActive == active &&
		previousProv == string(sample.Provenance) {
		if observed < previousAt {
			observed = previousAt
		}
		_, updateErr := s.db.Exec(`UPDATE limit_sample SET last_seen=?
          WHERE tool=? AND kind=? AND scope=? AND observed_at=?`,
			observed, string(sample.Tool), string(sample.Kind), sample.Scope, previousAt)
		return false, updateErr
	}

	result, err := s.db.Exec(`INSERT INTO limit_sample
      (tool,kind,scope,percent,resets_at,is_active,observed_at,last_seen,provenance)
      VALUES(?,?,?,?,?,?,?,?,?)
      ON CONFLICT(tool,kind,scope,observed_at) DO UPDATE SET
        percent=excluded.percent, resets_at=excluded.resets_at,
        is_active=excluded.is_active, last_seen=excluded.last_seen,
        provenance=excluded.provenance`,
		string(sample.Tool), string(sample.Kind), sample.Scope, sample.Percent,
		reset, active, observed, observed, string(sample.Provenance))
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}

func nullableUnix(t *time.Time) sql.NullInt64 {
	if t == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: t.Unix(), Valid: true}
}

func nullIntEqual(a, b sql.NullInt64) bool {
	return a.Valid == b.Valid && (!a.Valid || a.Int64 == b.Int64)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
