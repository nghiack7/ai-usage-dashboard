package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type Tool struct {
	Name               string  `json:"name"`
	DisplayName        string  `json:"display_name"`
	Enabled            bool    `json:"enabled"`
	MonthlyCostUSD     float64 `json:"monthly_cost_usd"`
	MonthlyQuotaTokens int64   `json:"monthly_quota_tokens"`
}

type Event struct {
	EventHash        string    `json:"event_hash"`
	ToolName         string    `json:"tool_name"`
	SourcePath       string    `json:"source_path"`
	SourceLine       int       `json:"source_line"`
	OccurredAt       time.Time `json:"occurred_at"`
	Project          string    `json:"project"`
	Model            string    `json:"model"`
	InputTokens      int64     `json:"input_tokens"`
	OutputTokens     int64     `json:"output_tokens"`
	CacheReadTokens  int64     `json:"cache_read_tokens"`
	CacheWriteTokens int64     `json:"cache_write_tokens"`
	TotalTokens      int64     `json:"total_tokens"`
	RawKind          string    `json:"raw_kind"`
}

type ScanRun struct {
	ID             int64      `json:"id"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	Status         string     `json:"status"`
	FilesSeen      int64      `json:"files_seen"`
	EventsSeen     int64      `json:"events_seen"`
	EventsInserted int64      `json:"events_inserted"`
	Error          string     `json:"error,omitempty"`
}

type FileState struct {
	SourcePath string
	SizeBytes  int64
	ModTime    time.Time
	Offset     int64
	LineCount  int
}

type Totals struct {
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
	Events           int64 `json:"events"`
}

type ToolSummary struct {
	Tool
	Totals
	UsagePercent      float64 `json:"usage_percent"`
	EstimatedValueUSD float64 `json:"estimated_value_usd"`
}

type DaySummary struct {
	Date string `json:"date"`
	Totals
}

type Summary struct {
	Days         int           `json:"days"`
	GeneratedAt  time.Time     `json:"generated_at"`
	Totals       Totals        `json:"totals"`
	Tools        []ToolSummary `json:"tools"`
	Daily        []DaySummary  `json:"daily"`
	RecentEvents []Event       `json:"recent_events"`
	Scans        []ScanRun     `json:"scans"`
}

func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database %q: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite database %q: %w", path, err)
	}
	s := &Store{db: db}
	if err := s.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Migrate(ctx context.Context) error {
	stmts := []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA foreign_keys = ON`,
		`CREATE TABLE IF NOT EXISTS tools (
			name TEXT PRIMARY KEY,
			display_name TEXT NOT NULL,
			enabled INTEGER NOT NULL,
			monthly_cost_usd REAL NOT NULL,
			monthly_quota_tokens INTEGER NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_hash TEXT NOT NULL UNIQUE,
			tool_name TEXT NOT NULL,
			source_path TEXT NOT NULL,
			source_line INTEGER NOT NULL,
			occurred_at TEXT NOT NULL,
			project TEXT NOT NULL,
			model TEXT NOT NULL,
			input_tokens INTEGER NOT NULL,
			output_tokens INTEGER NOT NULL,
			cache_read_tokens INTEGER NOT NULL,
			cache_write_tokens INTEGER NOT NULL,
			total_tokens INTEGER NOT NULL,
			raw_kind TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_occurred_at ON events(occurred_at)`,
		`CREATE INDEX IF NOT EXISTS idx_events_tool_occurred ON events(tool_name, occurred_at)`,
		`CREATE TABLE IF NOT EXISTS scan_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			started_at TEXT NOT NULL,
			finished_at TEXT,
			status TEXT NOT NULL,
			files_seen INTEGER NOT NULL,
			events_seen INTEGER NOT NULL,
			events_inserted INTEGER NOT NULL,
			error TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS file_states (
			source_path TEXT PRIMARY KEY,
			size_bytes INTEGER NOT NULL,
			mod_time TEXT NOT NULL,
			offset INTEGER NOT NULL,
			line_count INTEGER NOT NULL,
			updated_at TEXT NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("run migration statement %q: %w", stmt, err)
		}
	}
	return nil
}

func (s *Store) GetFileState(ctx context.Context, sourcePath string, currentSize int64) (FileState, error) {
	row := s.db.QueryRowContext(ctx, `SELECT source_path, size_bytes, mod_time, offset, line_count
		FROM file_states WHERE source_path = ?`, sourcePath)
	var state FileState
	var modTime string
	if err := row.Scan(&state.SourcePath, &state.SizeBytes, &modTime, &state.Offset, &state.LineCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return FileState{SourcePath: sourcePath}, nil
		}
		return FileState{}, fmt.Errorf("read file scan state for %q: %w", sourcePath, err)
	}
	parsed, err := time.Parse(time.RFC3339Nano, modTime)
	if err != nil {
		return FileState{}, fmt.Errorf("parse file state mod_time for %q: %w", sourcePath, err)
	}
	state.ModTime = parsed
	if state.Offset < 0 || state.Offset > currentSize || state.SizeBytes > currentSize {
		return FileState{SourcePath: sourcePath}, nil
	}
	return state, nil
}

func (s *Store) SaveFileState(ctx context.Context, state FileState) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO file_states (
		source_path, size_bytes, mod_time, offset, line_count, updated_at
	) VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(source_path) DO UPDATE SET
		size_bytes = excluded.size_bytes,
		mod_time = excluded.mod_time,
		offset = excluded.offset,
		line_count = excluded.line_count,
		updated_at = excluded.updated_at`,
		state.SourcePath,
		state.SizeBytes,
		state.ModTime.UTC().Format(time.RFC3339Nano),
		state.Offset,
		state.LineCount,
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("save file scan state for %q: %w", state.SourcePath, err)
	}
	return nil
}

func (s *Store) UpsertTool(ctx context.Context, tool Tool) error {
	if tool.DisplayName == "" {
		tool.DisplayName = tool.Name
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO tools (
		name, display_name, enabled, monthly_cost_usd, monthly_quota_tokens, updated_at
	) VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(name) DO UPDATE SET
		display_name = excluded.display_name,
		enabled = excluded.enabled,
		monthly_cost_usd = excluded.monthly_cost_usd,
		monthly_quota_tokens = excluded.monthly_quota_tokens,
		updated_at = excluded.updated_at`,
		tool.Name,
		tool.DisplayName,
		boolToInt(tool.Enabled),
		tool.MonthlyCostUSD,
		tool.MonthlyQuotaTokens,
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("upsert tool %q: %w", tool.Name, err)
	}
	return nil
}

func (s *Store) InsertEvent(ctx context.Context, ev Event) (bool, error) {
	if ev.TotalTokens == 0 {
		ev.TotalTokens = ev.InputTokens + ev.OutputTokens + ev.CacheReadTokens + ev.CacheWriteTokens
	}
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO events (
		event_hash, tool_name, source_path, source_line, occurred_at, project, model,
		input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, total_tokens,
		raw_kind, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.EventHash,
		ev.ToolName,
		ev.SourcePath,
		ev.SourceLine,
		ev.OccurredAt.UTC().Format(time.RFC3339Nano),
		ev.Project,
		ev.Model,
		ev.InputTokens,
		ev.OutputTokens,
		ev.CacheReadTokens,
		ev.CacheWriteTokens,
		ev.TotalTokens,
		ev.RawKind,
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return false, fmt.Errorf("insert usage event for tool %q from %q:%d: %w", ev.ToolName, ev.SourcePath, ev.SourceLine, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read insert result for event %q: %w", ev.EventHash, err)
	}
	return n > 0, nil
}

func (s *Store) StartScan(ctx context.Context) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `INSERT INTO scan_runs (
		started_at, status, files_seen, events_seen, events_inserted, error
	) VALUES (?, 'running', 0, 0, 0, '')`, now)
	if err != nil {
		return 0, fmt.Errorf("start scan run: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read scan run id: %w", err)
	}
	return id, nil
}

func (s *Store) FinishScan(ctx context.Context, id int64, status string, filesSeen, eventsSeen, eventsInserted int64, scanErr error) error {
	errText := ""
	if scanErr != nil {
		errText = scanErr.Error()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE scan_runs SET
		finished_at = ?, status = ?, files_seen = ?, events_seen = ?, events_inserted = ?, error = ?
		WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339Nano),
		status,
		filesSeen,
		eventsSeen,
		eventsInserted,
		errText,
		id,
	)
	if err != nil {
		return fmt.Errorf("finish scan run %d: %w", id, err)
	}
	return nil
}

func (s *Store) BuildSummary(ctx context.Context, days int) (Summary, error) {
	if days <= 0 || days > 366 {
		days = 30
	}
	since := time.Now().UTC().AddDate(0, 0, -days)
	summary := Summary{
		Days:        days,
		GeneratedAt: time.Now().UTC(),
	}

	totals, err := s.queryTotals(ctx, since)
	if err != nil {
		return Summary{}, err
	}
	summary.Totals = totals

	tools, err := s.queryToolSummaries(ctx, since)
	if err != nil {
		return Summary{}, err
	}
	summary.Tools = tools

	daily, err := s.queryDaily(ctx, since)
	if err != nil {
		return Summary{}, err
	}
	summary.Daily = daily

	events, err := s.queryRecentEvents(ctx, since, 80)
	if err != nil {
		return Summary{}, err
	}
	summary.RecentEvents = events

	scans, err := s.queryScans(ctx, 10)
	if err != nil {
		return Summary{}, err
	}
	summary.Scans = scans
	return summary, nil
}

func (s *Store) queryTotals(ctx context.Context, since time.Time) (Totals, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(input_tokens), 0),
		COALESCE(SUM(output_tokens), 0),
		COALESCE(SUM(cache_read_tokens), 0),
		COALESCE(SUM(cache_write_tokens), 0),
		COALESCE(SUM(total_tokens), 0),
		COUNT(*)
		FROM events WHERE occurred_at >= ?`, since.UTC().Format(time.RFC3339Nano))
	var totals Totals
	if err := row.Scan(&totals.InputTokens, &totals.OutputTokens, &totals.CacheReadTokens, &totals.CacheWriteTokens, &totals.TotalTokens, &totals.Events); err != nil {
		return Totals{}, fmt.Errorf("query total usage: %w", err)
	}
	return totals, nil
}

func (s *Store) queryToolSummaries(ctx context.Context, since time.Time) ([]ToolSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT
		t.name, t.display_name, t.enabled, t.monthly_cost_usd, t.monthly_quota_tokens,
		COALESCE(SUM(e.input_tokens), 0),
		COALESCE(SUM(e.output_tokens), 0),
		COALESCE(SUM(e.cache_read_tokens), 0),
		COALESCE(SUM(e.cache_write_tokens), 0),
		COALESCE(SUM(e.total_tokens), 0),
		COUNT(e.id)
		FROM tools t
		LEFT JOIN events e ON e.tool_name = t.name AND e.occurred_at >= ?
		GROUP BY t.name
		ORDER BY COALESCE(SUM(e.total_tokens), 0) DESC, t.name ASC`, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("query tool usage: %w", err)
	}
	defer rows.Close()

	var out []ToolSummary
	for rows.Next() {
		var item ToolSummary
		var enabled int
		if err := rows.Scan(
			&item.Name,
			&item.DisplayName,
			&enabled,
			&item.MonthlyCostUSD,
			&item.MonthlyQuotaTokens,
			&item.InputTokens,
			&item.OutputTokens,
			&item.CacheReadTokens,
			&item.CacheWriteTokens,
			&item.TotalTokens,
			&item.Events,
		); err != nil {
			return nil, fmt.Errorf("scan tool usage row: %w", err)
		}
		item.Enabled = enabled == 1
		if item.MonthlyQuotaTokens > 0 {
			item.UsagePercent = float64(item.TotalTokens) / float64(item.MonthlyQuotaTokens) * 100
		}
		if item.MonthlyQuotaTokens > 0 && item.MonthlyCostUSD > 0 {
			item.EstimatedValueUSD = float64(item.TotalTokens) / float64(item.MonthlyQuotaTokens) * item.MonthlyCostUSD
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tool usage rows: %w", err)
	}
	return out, nil
}

func (s *Store) queryDaily(ctx context.Context, since time.Time) ([]DaySummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT
		substr(occurred_at, 1, 10),
		COALESCE(SUM(input_tokens), 0),
		COALESCE(SUM(output_tokens), 0),
		COALESCE(SUM(cache_read_tokens), 0),
		COALESCE(SUM(cache_write_tokens), 0),
		COALESCE(SUM(total_tokens), 0),
		COUNT(*)
		FROM events
		WHERE occurred_at >= ?
		GROUP BY substr(occurred_at, 1, 10)
		ORDER BY substr(occurred_at, 1, 10) ASC`, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("query daily usage: %w", err)
	}
	defer rows.Close()

	var out []DaySummary
	for rows.Next() {
		var item DaySummary
		if err := rows.Scan(&item.Date, &item.InputTokens, &item.OutputTokens, &item.CacheReadTokens, &item.CacheWriteTokens, &item.TotalTokens, &item.Events); err != nil {
			return nil, fmt.Errorf("scan daily usage row: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate daily usage rows: %w", err)
	}
	return out, nil
}

func (s *Store) queryRecentEvents(ctx context.Context, since time.Time, limit int) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT
		event_hash, tool_name, source_path, source_line, occurred_at, project, model,
		input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, total_tokens, raw_kind
		FROM events
		WHERE occurred_at >= ?
		ORDER BY occurred_at DESC, id DESC
		LIMIT ?`, since.UTC().Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, fmt.Errorf("query recent usage events: %w", err)
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var item Event
		var occurred string
		if err := rows.Scan(
			&item.EventHash,
			&item.ToolName,
			&item.SourcePath,
			&item.SourceLine,
			&occurred,
			&item.Project,
			&item.Model,
			&item.InputTokens,
			&item.OutputTokens,
			&item.CacheReadTokens,
			&item.CacheWriteTokens,
			&item.TotalTokens,
			&item.RawKind,
		); err != nil {
			return nil, fmt.Errorf("scan recent usage event: %w", err)
		}
		t, err := time.Parse(time.RFC3339Nano, occurred)
		if err != nil {
			return nil, fmt.Errorf("parse event timestamp %q: %w", occurred, err)
		}
		item.OccurredAt = t
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent usage events: %w", err)
	}
	return out, nil
}

func (s *Store) queryScans(ctx context.Context, limit int) ([]ScanRun, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT
		id, started_at, finished_at, status, files_seen, events_seen, events_inserted, error
		FROM scan_runs
		ORDER BY id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("query scan runs: %w", err)
	}
	defer rows.Close()

	var out []ScanRun
	for rows.Next() {
		var item ScanRun
		var started string
		var finished sql.NullString
		if err := rows.Scan(&item.ID, &started, &finished, &item.Status, &item.FilesSeen, &item.EventsSeen, &item.EventsInserted, &item.Error); err != nil {
			return nil, fmt.Errorf("scan scan-run row: %w", err)
		}
		startedAt, err := time.Parse(time.RFC3339Nano, started)
		if err != nil {
			return nil, fmt.Errorf("parse scan started_at %q: %w", started, err)
		}
		item.StartedAt = startedAt
		if finished.Valid {
			finishedAt, err := time.Parse(time.RFC3339Nano, finished.String)
			if err != nil {
				return nil, fmt.Errorf("parse scan finished_at %q: %w", finished.String, err)
			}
			item.FinishedAt = &finishedAt
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate scan-run rows: %w", err)
	}
	return out, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

var ErrNotFound = errors.New("not found")
