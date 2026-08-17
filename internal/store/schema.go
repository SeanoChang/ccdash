package store

const schemaSQL = `
CREATE TABLE IF NOT EXISTS request (
  id TEXT PRIMARY KEY,
  tool TEXT NOT NULL,
  ts INTEGER NOT NULL,
  model TEXT NOT NULL,
  project TEXT,
  session TEXT,
  agent TEXT,
  workflow TEXT,
  depth INTEGER NOT NULL DEFAULT 0,
  in_tok INTEGER NOT NULL DEFAULT 0,
  out_tok INTEGER NOT NULL DEFAULT 0,
  think_tok INTEGER NOT NULL DEFAULT 0,
  cache_read INTEGER NOT NULL DEFAULT 0,
  cache_w5m INTEGER NOT NULL DEFAULT 0,
  cache_w1h INTEGER NOT NULL DEFAULT 0,
  anomaly INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS request_ts ON request(ts);
CREATE INDEX IF NOT EXISTS request_project ON request(project, ts);
CREATE INDEX IF NOT EXISTS request_model ON request(model, ts);
CREATE INDEX IF NOT EXISTS request_session  ON request(session, ts);
CREATE INDEX IF NOT EXISTS request_agent    ON request(agent, ts);
CREATE INDEX IF NOT EXISTS request_workflow ON request(workflow, ts);

-- This table is deliberately not foreign-keyed to request. Source transcripts
-- are pruned, while archived request rows must survive indefinitely.
CREATE TABLE IF NOT EXISTS source_file (
  path TEXT PRIMARY KEY,
  tool TEXT NOT NULL,
  size INTEGER NOT NULL,
  mtime INTEGER NOT NULL,
  offset INTEGER NOT NULL,
  last_seen INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS unpriced (
  model TEXT PRIMARY KEY,
  count INTEGER NOT NULL,
  first_seen INTEGER NOT NULL,
  last_seen INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS limit_sample (
  tool TEXT NOT NULL,
  kind TEXT NOT NULL,
  scope TEXT NOT NULL DEFAULT '',
  percent REAL NOT NULL,
  resets_at INTEGER,
  is_active INTEGER NOT NULL DEFAULT 0,
  observed_at INTEGER NOT NULL,
  last_seen INTEGER NOT NULL,
  provenance TEXT NOT NULL,
  PRIMARY KEY (tool, kind, scope, observed_at)
);
CREATE INDEX IF NOT EXISTS limit_latest
  ON limit_sample(tool, kind, scope, observed_at DESC);

CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO meta(key, value) VALUES('schema_version', '2')
  ON CONFLICT(key) DO NOTHING;
`
