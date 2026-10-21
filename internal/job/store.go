package job

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const historyLimit = 500

const schemaJobs = `CREATE TABLE IF NOT EXISTS jobs (
  id           TEXT PRIMARY KEY,
  type         TEXT NOT NULL,
  priority     INTEGER NOT NULL,
  payload      TEXT NOT NULL,
  attempts     INTEGER NOT NULL DEFAULT 0,
  last_err     TEXT NOT NULL DEFAULT '',
  received_at  INTEGER NOT NULL,
  final_status TEXT,
  final_error  TEXT NOT NULL DEFAULT '',
  printer      TEXT NOT NULL DEFAULT '',
  completed_at INTEGER
)`

const schemaHistory = `CREATE TABLE IF NOT EXISTS history (
  id           TEXT NOT NULL,
  type         TEXT NOT NULL,
  status       TEXT NOT NULL,
  printer      TEXT NOT NULL,
  attempts     INTEGER NOT NULL,
  error        TEXT NOT NULL,
  received_at  INTEGER NOT NULL,
  completed_at INTEGER NOT NULL
)`

const indexHistoryID = `CREATE INDEX IF NOT EXISTS history_id ON history (id)`

// Record is a finished job as shown on the Jobs page.
type Record struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Status      string    `json:"status"`
	Printer     string    `json:"printer"`
	Attempts    int       `json:"attempts"`
	Error       string    `json:"error,omitempty"`
	ReceivedAt  time.Time `json:"receivedAt"`
	CompletedAt time.Time `json:"completedAt"`
}

// Final is a finished job whose status the backend hasn't acknowledged yet.
type Final struct {
	ID     string
	Status string
	Error  string
}

// LaneState is a per-printer snapshot for /api/status.
type LaneState struct {
	Name    string `json:"name"`
	Online  bool   `json:"online"`
	Pending int    `json:"pending"`
}

// Store keeps claimed jobs on disk. The backend watchdog never resets a job
// stuck at PRINTING, so without this a killed process would lose it.
type Store struct {
	db *sql.DB
}

func OpenStore(path string) (*Store, error) {
	db, err := sql.Open(sqlDriver, path)
	if err != nil {
		return nil, fmt.Errorf("open job store: %w", err)
	}
	// one connection serializes writers — no "database is locked" between goroutines
	db.SetMaxOpenConns(1)

	for _, stmt := range []string{schemaJobs, schemaHistory, indexHistoryID} {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return nil, fmt.Errorf("init job store schema: %w", err)
		}
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// Insert reports false when the job is already held — a redispatch of the same jobId
func (s *Store) Insert(j *Job) (bool, error) {
	payload, err := json.Marshal(j.Payload)
	if err != nil {
		return false, fmt.Errorf("marshal payload %s: %w", j.ID, err)
	}

	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO jobs (id, type, priority, payload, attempts, last_err, received_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		j.ID, j.Type, j.Priority, string(payload), j.Attempts, j.LastErr, j.ReceivedAt.UnixMilli(),
	)
	if err != nil {
		return false, fmt.Errorf("insert job %s: %w", j.ID, err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("insert job %s: %w", j.ID, err)
	}
	return n == 1, nil
}

// Lookup reports whether id is held locally and, once it finished printing, its
// final status. Archived jobs count too: a 409'd final gets archived, and the
// backend may hand the same job back after its claim expires.
func (s *Store) Lookup(id string) (bool, *Final, error) {
	var status sql.NullString
	var finalErr string

	err := s.db.QueryRow(`SELECT final_status, final_error FROM jobs WHERE id = ?`, id).Scan(&status, &finalErr)
	if errors.Is(err, sql.ErrNoRows) {
		err = s.db.QueryRow(
			`SELECT status, error FROM history WHERE id = ? ORDER BY completed_at DESC LIMIT 1`, id,
		).Scan(&status, &finalErr)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, fmt.Errorf("lookup job %s: %w", id, err)
	}

	if status.Valid {
		return true, &Final{ID: id, Status: status.String, Error: finalErr}, nil
	}
	return true, nil, nil
}

func (s *Store) RecordAttempt(id string, attempts int, lastErr string) error {
	if _, err := s.db.Exec(`UPDATE jobs SET attempts = ?, last_err = ? WHERE id = ?`, attempts, lastErr, id); err != nil {
		return fmt.Errorf("record attempt %s: %w", id, err)
	}
	return nil
}

func (s *Store) SetFinal(id, status, errMsg, printer string, attempts int, at time.Time) error {
	_, err := s.db.Exec(
		`UPDATE jobs SET final_status = ?, final_error = ?, printer = ?, attempts = ?, completed_at = ? WHERE id = ?`,
		status, errMsg, printer, attempts, at.UnixMilli(), id,
	)
	if err != nil {
		return fmt.Errorf("set final %s: %w", id, err)
	}
	return nil
}

// Unfinished returns jobs that still need printing, oldest first
func (s *Store) Unfinished() ([]*Job, error) {
	rows, err := s.db.Query(
		`SELECT id, type, priority, payload, attempts, last_err, received_at
		 FROM jobs WHERE final_status IS NULL ORDER BY received_at`,
	)
	if err != nil {
		return nil, fmt.Errorf("query unfinished jobs: %w", err)
	}
	defer rows.Close()

	var out []*Job
	for rows.Next() {
		var j Job
		var payload string
		var received int64
		if err := rows.Scan(&j.ID, &j.Type, &j.Priority, &payload, &j.Attempts, &j.LastErr, &received); err != nil {
			return nil, fmt.Errorf("scan unfinished job: %w", err)
		}
		if err := json.Unmarshal([]byte(payload), &j.Payload); err != nil {
			return nil, fmt.Errorf("unmarshal payload %s: %w", j.ID, err)
		}
		j.ReceivedAt = time.UnixMilli(received)
		out = append(out, &j)
	}
	return out, rows.Err()
}

func (s *Store) PendingFinals() ([]Final, error) {
	rows, err := s.db.Query(`SELECT id, final_status, final_error FROM jobs WHERE final_status IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("query pending finals: %w", err)
	}
	defer rows.Close()

	var out []Final
	for rows.Next() {
		var f Final
		if err := rows.Scan(&f.ID, &f.Status, &f.Error); err != nil {
			return nil, fmt.Errorf("scan pending final: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Archive moves a finished job into history once the backend has its final status
func (s *Store) Archive(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("archive %s: %w", id, err)
	}
	defer tx.Rollback()

	stmts := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO history (id, type, status, printer, attempts, error, received_at, completed_at)
		  SELECT id, type, final_status, printer, attempts, final_error, received_at, completed_at
		  FROM jobs WHERE id = ? AND final_status IS NOT NULL`, []any{id}},
		{`DELETE FROM jobs WHERE id = ? AND final_status IS NOT NULL`, []any{id}},
		{`DELETE FROM history WHERE rowid NOT IN
		  (SELECT rowid FROM history ORDER BY completed_at DESC, rowid DESC LIMIT ?)`, []any{historyLimit}},
	}
	for _, st := range stmts {
		if _, err := tx.Exec(st.sql, st.args...); err != nil {
			return fmt.Errorf("archive %s: %w", id, err)
		}
	}
	return tx.Commit()
}

// History returns archived records, newest first
func (s *Store) History() ([]Record, error) {
	rows, err := s.db.Query(
		`SELECT id, type, status, printer, attempts, error, received_at, completed_at
		 FROM history ORDER BY completed_at DESC, rowid DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query history: %w", err)
	}
	defer rows.Close()

	var out []Record
	for rows.Next() {
		var r Record
		var received, completed int64
		if err := rows.Scan(&r.ID, &r.Type, &r.Status, &r.Printer, &r.Attempts, &r.Error, &received, &completed); err != nil {
			return nil, fmt.Errorf("scan history: %w", err)
		}
		r.ReceivedAt = time.UnixMilli(received)
		r.CompletedAt = time.UnixMilli(completed)
		out = append(out, r)
	}
	return out, rows.Err()
}
