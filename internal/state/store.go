package state

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Fixed-width UTC timestamps keep SQLite text comparisons in chronological order.
const sqliteTimeLayout = "2006-01-02T15:04:05.000000000Z"

type Store struct {
	db *sql.DB
}

type ThreadRecord struct {
	ThreadID      string
	Board         string
	PostID        int64
	CreatedAt     time.Time
	LastBumpedAt  time.Time
	LastSeenAt    time.Time
	NotifiedNewAt *time.Time
	ReplyPosts    int
	ReplyFiles    int
	Ignored       bool
	HasOP         bool
}

type GatewayNotification struct {
	ID        int64
	ThreadID  string
	ChatID    int64
	Text      string
	ParseMode string
	Attempts  int
}

type StreamState struct {
	Key             string
	Active          bool
	LiveNotified    bool
	Consecutive404s int
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1)

	store := &Store{db: db}
	if err := store.initSchema(context.Background()); err != nil {
		db.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) PruneGatewayBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin gateway prune: %w", err)
	}
	defer tx.Rollback()

	total := int64(0)
	for _, statement := range []string{
		`DELETE FROM threads WHERE last_seen_at < ?;`,
		`DELETE FROM gateway_events WHERE processed_at < ?;`,
		`DELETE FROM gateway_notifications WHERE status = 'sent' AND sent_at < ?;`,
	} {
		result, err := tx.ExecContext(ctx, statement, formatTime(cutoff))
		if err != nil {
			return 0, fmt.Errorf("prune gateway data: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("count pruned gateway data: %w", err)
		}
		total += rows
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit gateway prune: %w", err)
	}
	return total, nil
}

func (s *Store) initSchema(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS threads (
  thread_id TEXT PRIMARY KEY,
  board TEXT NOT NULL,
  post_id INTEGER NOT NULL,
  created_at TEXT NOT NULL DEFAULT '1970-01-01T00:00:00.000000000Z',
  last_bumped_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  notified_new_at TEXT,
  reply_posts INTEGER NOT NULL DEFAULT 0,
  reply_files INTEGER NOT NULL DEFAULT 0,
  ignored INTEGER NOT NULL DEFAULT 0,
  has_op INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_threads_post_id ON threads(post_id);
CREATE INDEX IF NOT EXISTS idx_threads_last_seen_at ON threads(last_seen_at);

CREATE TABLE IF NOT EXISTS stream_states (
  stream_key TEXT PRIMARY KEY,
  active INTEGER NOT NULL,
  live_notified INTEGER NOT NULL,
  consecutive_404s INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS cursors (
  name TEXT PRIMARY KEY,
  position INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS gateway_events (
  event_id TEXT PRIMARY KEY,
  processed_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS gateway_notifications (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  thread_id TEXT NOT NULL UNIQUE,
  chat_id INTEGER NOT NULL,
  text TEXT NOT NULL,
  parse_mode TEXT NOT NULL,
  status TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  sent_at TEXT
);
`

	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("init schema: %w", err)
	}
	if err := s.addColumnIfMissing(ctx, "threads", "reply_posts", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "threads", "created_at", "TEXT NOT NULL DEFAULT '1970-01-01T00:00:00.000000000Z'"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "threads", "reply_files", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "threads", "ignored", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "threads", "has_op", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	return nil
}

func (s *Store) addColumnIfMissing(ctx context.Context, table, column, definition string) error {
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info("+table+");")
	if err != nil {
		return fmt.Errorf("inspect %s columns: %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("scan %s columns: %w", table, err)
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("inspect %s columns: %w", table, err)
	}

	if _, err := s.db.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+column+" "+definition+";"); err != nil {
		return fmt.Errorf("add %s.%s: %w", table, column, err)
	}
	return nil
}

func (s *Store) GetThread(ctx context.Context, threadID string) (ThreadRecord, bool, error) {
	const query = `
SELECT thread_id, board, post_id, created_at, last_bumped_at, last_seen_at, notified_new_at, reply_posts, reply_files, ignored, has_op
FROM threads
WHERE thread_id = ?;
`

	var record ThreadRecord
	var createdRaw string
	var lastBumpedRaw string
	var lastSeenRaw string
	var notifiedRaw sql.NullString
	var ignored int
	var hasOP int

	err := s.db.QueryRowContext(ctx, query, threadID).Scan(
		&record.ThreadID,
		&record.Board,
		&record.PostID,
		&createdRaw,
		&lastBumpedRaw,
		&lastSeenRaw,
		&notifiedRaw,
		&record.ReplyPosts,
		&record.ReplyFiles,
		&ignored,
		&hasOP,
	)
	if err == sql.ErrNoRows {
		return ThreadRecord{}, false, nil
	}
	if err != nil {
		return ThreadRecord{}, false, fmt.Errorf("query thread: %w", err)
	}

	record.LastBumpedAt, err = parseTime(lastBumpedRaw)
	if err != nil {
		return ThreadRecord{}, false, fmt.Errorf("parse last_bumped_at: %w", err)
	}
	record.CreatedAt, err = parseTime(createdRaw)
	if err != nil {
		return ThreadRecord{}, false, fmt.Errorf("parse created_at: %w", err)
	}
	if record.CreatedAt.Equal(time.Unix(0, 0).UTC()) {
		record.CreatedAt = record.LastBumpedAt
	}
	record.LastSeenAt, err = parseTime(lastSeenRaw)
	if err != nil {
		return ThreadRecord{}, false, fmt.Errorf("parse last_seen_at: %w", err)
	}
	if notifiedRaw.Valid {
		parsed, err := parseTime(notifiedRaw.String)
		if err != nil {
			return ThreadRecord{}, false, fmt.Errorf("parse notified_new_at: %w", err)
		}
		record.NotifiedNewAt = &parsed
	}
	record.Ignored = ignored != 0
	record.HasOP = hasOP != 0

	return record, true, nil
}

type sqlExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func upsertThread(ctx context.Context, db sqlExecer, record ThreadRecord) error {
	const statement = `
INSERT INTO threads (thread_id, board, post_id, created_at, last_bumped_at, last_seen_at, notified_new_at, reply_posts, reply_files, ignored, has_op)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(thread_id) DO UPDATE SET
  board = excluded.board,
  post_id = excluded.post_id,
  created_at = excluded.created_at,
  last_bumped_at = excluded.last_bumped_at,
  last_seen_at = excluded.last_seen_at,
  notified_new_at = excluded.notified_new_at,
  reply_posts = excluded.reply_posts,
  reply_files = excluded.reply_files,
  ignored = excluded.ignored,
  has_op = excluded.has_op;
`

	var notified any
	if record.NotifiedNewAt != nil {
		notified = formatTime(*record.NotifiedNewAt)
	}

	_, err := db.ExecContext(
		ctx,
		statement,
		record.ThreadID,
		record.Board,
		record.PostID,
		formatTime(record.CreatedAt),
		formatTime(record.LastBumpedAt),
		formatTime(record.LastSeenAt),
		notified,
		record.ReplyPosts,
		record.ReplyFiles,
		boolToInt(record.Ignored),
		boolToInt(record.HasOP),
	)
	if err != nil {
		return fmt.Errorf("upsert thread: %w", err)
	}
	return nil
}

func (s *Store) GatewayEventProcessed(ctx context.Context, eventID string) (bool, error) {
	const query = `SELECT 1 FROM gateway_events WHERE event_id = ?;`

	var found int
	err := s.db.QueryRowContext(ctx, query, eventID).Scan(&found)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query gateway event: %w", err)
	}
	return true, nil
}

func (s *Store) StoreGatewayEvent(ctx context.Context, eventID string, record ThreadRecord, notification *GatewayNotification, processedAt time.Time) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin gateway event: %w", err)
	}
	defer tx.Rollback()

	var found int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM gateway_events WHERE event_id = ?;`, eventID).Scan(&found)
	if err == nil {
		return false, tx.Commit()
	}
	if err != sql.ErrNoRows {
		return false, fmt.Errorf("query gateway event: %w", err)
	}

	if err := upsertThread(ctx, tx, record); err != nil {
		return false, err
	}
	queued := false
	if notification != nil {
		result, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO gateway_notifications (thread_id, chat_id, text, parse_mode, status, attempts, next_attempt_at, created_at)
VALUES (?, ?, ?, ?, 'pending', 0, ?, ?);
`,
			notification.ThreadID,
			notification.ChatID,
			notification.Text,
			notification.ParseMode,
			formatTime(processedAt),
			formatTime(processedAt),
		)
		if err != nil {
			return false, fmt.Errorf("enqueue gateway notification: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return false, fmt.Errorf("count gateway notification enqueue: %w", err)
		}
		queued = rows > 0
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO gateway_events (event_id, processed_at) VALUES (?, ?);`, eventID, formatTime(processedAt)); err != nil {
		return false, fmt.Errorf("mark gateway event processed: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit gateway event: %w", err)
	}
	return queued, nil
}

func (s *Store) PendingGatewayNotifications(ctx context.Context, limit int, now time.Time) ([]GatewayNotification, error) {
	const query = `
SELECT id, thread_id, chat_id, text, parse_mode, attempts
FROM gateway_notifications
WHERE status = 'pending' AND next_attempt_at <= ?
ORDER BY id
LIMIT ?;
`
	rows, err := s.db.QueryContext(ctx, query, formatTime(now), limit)
	if err != nil {
		return nil, fmt.Errorf("query gateway notifications: %w", err)
	}
	defer rows.Close()

	var notifications []GatewayNotification
	for rows.Next() {
		var notification GatewayNotification
		if err := rows.Scan(&notification.ID, &notification.ThreadID, &notification.ChatID, &notification.Text, &notification.ParseMode, &notification.Attempts); err != nil {
			return nil, fmt.Errorf("scan gateway notification: %w", err)
		}
		notifications = append(notifications, notification)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query gateway notifications: %w", err)
	}
	return notifications, nil
}

func (s *Store) MarkGatewayNotificationSent(ctx context.Context, id int64, sentAt time.Time) error {
	const statement = `UPDATE gateway_notifications SET status = 'sent', sent_at = ? WHERE id = ?;`
	if _, err := s.db.ExecContext(ctx, statement, formatTime(sentAt), id); err != nil {
		return fmt.Errorf("mark gateway notification sent: %w", err)
	}
	return nil
}

func (s *Store) MarkGatewayNotificationFailed(ctx context.Context, id int64, attempts int, nextAttemptAt time.Time) error {
	const statement = `UPDATE gateway_notifications SET attempts = ?, next_attempt_at = ? WHERE id = ?;`
	if _, err := s.db.ExecContext(ctx, statement, attempts, formatTime(nextAttemptAt), id); err != nil {
		return fmt.Errorf("mark gateway notification failed: %w", err)
	}
	return nil
}

func (s *Store) EnsureGatewayBootstrapAt(ctx context.Context, now time.Time) (time.Time, error) {
	const cursor = "gateway_bootstrap_at"
	position, ok, err := s.GetCursor(ctx, cursor)
	if err != nil {
		return time.Time{}, err
	}
	if ok {
		const unixNanoThreshold = int64(1_000_000_000_000)
		if position < unixNanoThreshold {
			return time.Unix(position, 0).UTC(), nil
		}
		return time.Unix(0, position).UTC(), nil
	}
	bootstrapAt := now.UTC()
	if err := s.SetCursor(ctx, cursor, bootstrapAt.UnixNano()); err != nil {
		return time.Time{}, err
	}
	return bootstrapAt, nil
}

func (s *Store) GetStreamState(ctx context.Context, key string) (StreamState, bool, error) {
	const query = `
SELECT stream_key, active, live_notified, consecutive_404s
FROM stream_states
WHERE stream_key = ?;
`

	var stream StreamState
	var active int
	var liveNotified int

	err := s.db.QueryRowContext(ctx, query, key).Scan(
		&stream.Key,
		&active,
		&liveNotified,
		&stream.Consecutive404s,
	)
	if err == sql.ErrNoRows {
		return StreamState{}, false, nil
	}
	if err != nil {
		return StreamState{}, false, fmt.Errorf("query stream state: %w", err)
	}

	stream.Active = active != 0
	stream.LiveNotified = liveNotified != 0
	return stream, true, nil
}

func (s *Store) UpsertStreamState(ctx context.Context, stream StreamState) error {
	const statement = `
INSERT INTO stream_states (stream_key, active, live_notified, consecutive_404s)
VALUES (?, ?, ?, ?)
ON CONFLICT(stream_key) DO UPDATE SET
  active = excluded.active,
  live_notified = excluded.live_notified,
  consecutive_404s = excluded.consecutive_404s;
`

	_, err := s.db.ExecContext(
		ctx,
		statement,
		stream.Key,
		boolToInt(stream.Active),
		boolToInt(stream.LiveNotified),
		stream.Consecutive404s,
	)
	if err != nil {
		return fmt.Errorf("upsert stream state: %w", err)
	}

	return nil
}

func (s *Store) GetCursor(ctx context.Context, name string) (int64, bool, error) {
	const query = `SELECT position FROM cursors WHERE name = ?;`

	var position int64
	err := s.db.QueryRowContext(ctx, query, name).Scan(&position)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("query cursor: %w", err)
	}
	return position, true, nil
}

func (s *Store) SetCursor(ctx context.Context, name string, position int64) error {
	const statement = `
INSERT INTO cursors (name, position)
VALUES (?, ?)
ON CONFLICT(name) DO UPDATE SET position = excluded.position;
`

	if _, err := s.db.ExecContext(ctx, statement, name, position); err != nil {
		return fmt.Errorf("set cursor: %w", err)
	}
	return nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func formatTime(t time.Time) string {
	return t.UTC().Format(sqliteTimeLayout)
}

func parseTime(value string) (time.Time, error) {
	t, err := time.Parse(sqliteTimeLayout, value)
	if err == nil {
		return t, nil
	}

	// Accept older rows written before we switched to the fixed-width format.
	return time.Parse(time.RFC3339Nano, value)
}
