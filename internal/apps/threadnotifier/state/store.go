package state

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"martie/internal/storage"
)

type Store struct {
	db *storage.DB
}

type Thread struct {
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

type Notification struct {
	ID        int64
	ThreadID  string
	ChatID    int64
	Text      string
	ParseMode string
	Attempts  int
}

func New(db *storage.DB) (*Store, error) {
	store := &Store{db: db}
	if err := store.initSchema(context.Background()); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) initSchema(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS threadnotifier_threads (
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

CREATE INDEX IF NOT EXISTS idx_threadnotifier_threads_post_id ON threadnotifier_threads(post_id);
CREATE INDEX IF NOT EXISTS idx_threadnotifier_threads_last_seen_at ON threadnotifier_threads(last_seen_at);

CREATE TABLE IF NOT EXISTS threadnotifier_events (
  event_id TEXT PRIMARY KEY,
  processed_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS threadnotifier_notifications (
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

CREATE TABLE IF NOT EXISTS threadnotifier_bootstrap (
  name TEXT PRIMARY KEY,
  position INTEGER NOT NULL
);
`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("init threadnotifier schema: %w", err)
	}
	for _, column := range []struct {
		name       string
		definition string
	}{
		{"reply_posts", "INTEGER NOT NULL DEFAULT 0"},
		{"created_at", "TEXT NOT NULL DEFAULT '1970-01-01T00:00:00.000000000Z'"},
		{"reply_files", "INTEGER NOT NULL DEFAULT 0"},
		{"ignored", "INTEGER NOT NULL DEFAULT 0"},
		{"has_op", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := s.db.AddColumnIfMissing(ctx, "threadnotifier_threads", column.name, column.definition); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) PruneBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin threadnotifier prune: %w", err)
	}
	defer tx.Rollback()

	total := int64(0)
	for _, statement := range []string{
		`DELETE FROM threadnotifier_threads WHERE last_seen_at < ?;`,
		`DELETE FROM threadnotifier_events WHERE processed_at < ?;`,
		`DELETE FROM threadnotifier_notifications WHERE status = 'sent' AND sent_at < ?;`,
	} {
		result, err := tx.ExecContext(ctx, statement, storage.FormatTime(cutoff))
		if err != nil {
			return 0, fmt.Errorf("prune threadnotifier data: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("count pruned threadnotifier data: %w", err)
		}
		total += rows
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit threadnotifier prune: %w", err)
	}
	return total, nil
}

func (s *Store) GetThread(ctx context.Context, threadID string) (Thread, bool, error) {
	const query = `
SELECT thread_id, board, post_id, created_at, last_bumped_at, last_seen_at, notified_new_at, reply_posts, reply_files, ignored, has_op
FROM threadnotifier_threads
WHERE thread_id = ?;
`

	var record Thread
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
		return Thread{}, false, nil
	}
	if err != nil {
		return Thread{}, false, fmt.Errorf("query thread: %w", err)
	}

	record.LastBumpedAt, err = storage.ParseTime(lastBumpedRaw)
	if err != nil {
		return Thread{}, false, fmt.Errorf("parse last_bumped_at: %w", err)
	}
	record.CreatedAt, err = storage.ParseTime(createdRaw)
	if err != nil {
		return Thread{}, false, fmt.Errorf("parse created_at: %w", err)
	}
	if record.CreatedAt.Equal(time.Unix(0, 0).UTC()) {
		record.CreatedAt = record.LastBumpedAt
	}
	record.LastSeenAt, err = storage.ParseTime(lastSeenRaw)
	if err != nil {
		return Thread{}, false, fmt.Errorf("parse last_seen_at: %w", err)
	}
	if notifiedRaw.Valid {
		parsed, err := storage.ParseTime(notifiedRaw.String)
		if err != nil {
			return Thread{}, false, fmt.Errorf("parse notified_new_at: %w", err)
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

func upsertThread(ctx context.Context, db sqlExecer, record Thread) error {
	const statement = `
INSERT INTO threadnotifier_threads (thread_id, board, post_id, created_at, last_bumped_at, last_seen_at, notified_new_at, reply_posts, reply_files, ignored, has_op)
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
		notified = storage.FormatTime(*record.NotifiedNewAt)
	}

	_, err := db.ExecContext(
		ctx,
		statement,
		record.ThreadID,
		record.Board,
		record.PostID,
		storage.FormatTime(record.CreatedAt),
		storage.FormatTime(record.LastBumpedAt),
		storage.FormatTime(record.LastSeenAt),
		notified,
		record.ReplyPosts,
		record.ReplyFiles,
		storage.BoolToInt(record.Ignored),
		storage.BoolToInt(record.HasOP),
	)
	if err != nil {
		return fmt.Errorf("upsert thread: %w", err)
	}
	return nil
}

func (s *Store) StoreEvent(ctx context.Context, eventID string, record Thread, notification *Notification, processedAt time.Time) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin threadnotifier event: %w", err)
	}
	defer tx.Rollback()

	var found int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM threadnotifier_events WHERE event_id = ?;`, eventID).Scan(&found)
	if err == nil {
		return false, tx.Commit()
	}
	if err != sql.ErrNoRows {
		return false, fmt.Errorf("query threadnotifier event: %w", err)
	}

	if err := upsertThread(ctx, tx, record); err != nil {
		return false, err
	}
	queued := false
	if notification != nil {
		result, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO threadnotifier_notifications (thread_id, chat_id, text, parse_mode, status, attempts, next_attempt_at, created_at)
VALUES (?, ?, ?, ?, 'pending', 0, ?, ?);
`,
			notification.ThreadID,
			notification.ChatID,
			notification.Text,
			notification.ParseMode,
			storage.FormatTime(processedAt),
			storage.FormatTime(processedAt),
		)
		if err != nil {
			return false, fmt.Errorf("enqueue threadnotifier notification: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return false, fmt.Errorf("count threadnotifier notification enqueue: %w", err)
		}
		queued = rows > 0
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO threadnotifier_events (event_id, processed_at) VALUES (?, ?);`, eventID, storage.FormatTime(processedAt)); err != nil {
		return false, fmt.Errorf("mark threadnotifier event processed: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit threadnotifier event: %w", err)
	}
	return queued, nil
}

func (s *Store) PendingNotifications(ctx context.Context, limit int, now time.Time) ([]Notification, error) {
	const query = `
SELECT id, thread_id, chat_id, text, parse_mode, attempts
FROM threadnotifier_notifications
WHERE status = 'pending' AND next_attempt_at <= ?
ORDER BY id
LIMIT ?;
`
	rows, err := s.db.QueryContext(ctx, query, storage.FormatTime(now), limit)
	if err != nil {
		return nil, fmt.Errorf("query threadnotifier notifications: %w", err)
	}
	defer rows.Close()

	var notifications []Notification
	for rows.Next() {
		var notification Notification
		if err := rows.Scan(&notification.ID, &notification.ThreadID, &notification.ChatID, &notification.Text, &notification.ParseMode, &notification.Attempts); err != nil {
			return nil, fmt.Errorf("scan threadnotifier notification: %w", err)
		}
		notifications = append(notifications, notification)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query threadnotifier notifications: %w", err)
	}
	return notifications, nil
}

func (s *Store) MarkNotificationSent(ctx context.Context, id int64, sentAt time.Time) error {
	const statement = `UPDATE threadnotifier_notifications SET status = 'sent', sent_at = ? WHERE id = ?;`
	if _, err := s.db.ExecContext(ctx, statement, storage.FormatTime(sentAt), id); err != nil {
		return fmt.Errorf("mark threadnotifier notification sent: %w", err)
	}
	return nil
}

func (s *Store) MarkNotificationFailed(ctx context.Context, id int64, attempts int, nextAttemptAt time.Time) error {
	const statement = `UPDATE threadnotifier_notifications SET attempts = ?, next_attempt_at = ? WHERE id = ?;`
	if _, err := s.db.ExecContext(ctx, statement, attempts, storage.FormatTime(nextAttemptAt), id); err != nil {
		return fmt.Errorf("mark threadnotifier notification failed: %w", err)
	}
	return nil
}

func (s *Store) EnsureBootstrapAt(ctx context.Context, now time.Time) (time.Time, error) {
	const cursor = "gateway_bootstrap_at"
	position, ok, err := s.getBootstrap(ctx, cursor)
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
	if err := s.setBootstrap(ctx, cursor, bootstrapAt.UnixNano()); err != nil {
		return time.Time{}, err
	}
	return bootstrapAt, nil
}

func (s *Store) getBootstrap(ctx context.Context, name string) (int64, bool, error) {
	const query = `SELECT position FROM threadnotifier_bootstrap WHERE name = ?;`

	var position int64
	err := s.db.QueryRowContext(ctx, query, name).Scan(&position)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("query threadnotifier bootstrap: %w", err)
	}
	return position, true, nil
}

func (s *Store) setBootstrap(ctx context.Context, name string, position int64) error {
	const statement = `
INSERT INTO threadnotifier_bootstrap (name, position)
VALUES (?, ?)
ON CONFLICT(name) DO UPDATE SET position = excluded.position;
`

	if _, err := s.db.ExecContext(ctx, statement, name, position); err != nil {
		return fmt.Errorf("set threadnotifier bootstrap: %w", err)
	}
	return nil
}
