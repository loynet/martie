package state

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"martie/internal/storage"
)

type EventStatus string

const (
	EventAdmitted    EventStatus = "admitted"
	EventProcessing  EventStatus = "processing"
	EventPosting     EventStatus = "posting"
	EventPosted      EventStatus = "posted"
	EventFailedFinal EventStatus = "failed_final"
	EventUnknown     EventStatus = "unknown"
)

type Event struct {
	EventID      string
	Status       EventStatus
	Board        string
	ThreadID     int64
	PostID       int64
	ErrorCode    string
	ErrorMessage string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Store struct {
	db *storage.DB
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
CREATE TABLE IF NOT EXISTS channer_events (
  event_id TEXT PRIMARY KEY,
  status TEXT NOT NULL,
  board TEXT NOT NULL,
  thread_id INTEGER NOT NULL,
  post_id INTEGER NOT NULL,
  error_code TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("init channer schema: %w", err)
	}
	return nil
}

func (s *Store) StoreEvent(ctx context.Context, event Event) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
INSERT OR IGNORE INTO channer_events (
  event_id,
  status,
  board,
  thread_id,
  post_id,
  error_code,
  error_message,
  created_at,
  updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);
`,
		event.EventID,
		event.Status,
		event.Board,
		event.ThreadID,
		event.PostID,
		event.ErrorCode,
		event.ErrorMessage,
		storage.FormatTime(event.CreatedAt),
		storage.FormatTime(event.UpdatedAt),
	)
	if err != nil {
		return false, fmt.Errorf("store channer event: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count channer event insert: %w", err)
	}
	return rows > 0, nil
}

func (s *Store) ClaimEvent(ctx context.Context, eventID string, now time.Time) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
UPDATE channer_events
SET status = ?, updated_at = ?
WHERE event_id = ? AND status = ?;
`,
		EventProcessing,
		storage.FormatTime(now),
		eventID,
		EventAdmitted,
	)
	if err != nil {
		return false, fmt.Errorf("claim channer event: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count channer event claim: %w", err)
	}
	return rows > 0, nil
}

func (s *Store) MarkEventPosting(ctx context.Context, eventID string, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE channer_events
SET status = ?, updated_at = ?
WHERE event_id = ? AND status = ?;
`,
		EventPosting,
		storage.FormatTime(now),
		eventID,
		EventProcessing,
	)
	if err != nil {
		return fmt.Errorf("mark channer event posting: %w", err)
	}
	return requireAffected(result, "channer event posting")
}

func (s *Store) MarkEventPosted(ctx context.Context, eventID string, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE channer_events
SET status = ?, error_code = '', error_message = '', updated_at = ?
WHERE event_id = ? AND status = ?;
`,
		EventPosted,
		storage.FormatTime(now),
		eventID,
		EventPosting,
	)
	if err != nil {
		return fmt.Errorf("mark channer event posted: %w", err)
	}
	return requireAffected(result, "channer event posted")
}

func (s *Store) MarkEventFailed(ctx context.Context, eventID, code, message string, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE channer_events
SET status = ?, error_code = ?, error_message = ?, updated_at = ?
WHERE event_id = ? AND status IN (?, ?);
`,
		EventFailedFinal,
		code,
		message,
		storage.FormatTime(now),
		eventID,
		EventProcessing,
		EventPosting,
	)
	if err != nil {
		return fmt.Errorf("mark channer event failed: %w", err)
	}
	return requireAffected(result, "channer event failed")
}

func (s *Store) MarkEventUnknown(ctx context.Context, eventID, code, message string, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE channer_events
SET status = ?, error_code = ?, error_message = ?, updated_at = ?
WHERE event_id = ? AND status = ?;
`,
		EventUnknown,
		code,
		message,
		storage.FormatTime(now),
		eventID,
		EventPosting,
	)
	if err != nil {
		return fmt.Errorf("mark channer event unknown: %w", err)
	}
	return requireAffected(result, "channer event unknown")
}

func requireAffected(result sql.Result, action string) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count %s: %w", action, err)
	}
	if rows == 0 {
		return fmt.Errorf("%s: event not found", action)
	}
	return nil
}

func (s *Store) GetEvent(ctx context.Context, eventID string) (Event, bool, error) {
	const query = `
SELECT event_id, status, board, thread_id, post_id, error_code, error_message, created_at, updated_at
FROM channer_events
WHERE event_id = ?;
`

	event, err := scanEvent(s.db.QueryRowContext(ctx, query, eventID))
	if err == sql.ErrNoRows {
		return Event{}, false, nil
	}
	if err != nil {
		return Event{}, false, err
	}
	return event, true, nil
}

func (s *Store) PruneBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM channer_events WHERE updated_at < ?;`, storage.FormatTime(cutoff))
	if err != nil {
		return 0, fmt.Errorf("prune channer events: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count pruned channer events: %w", err)
	}
	return rows, nil
}

type eventScanner interface {
	Scan(...any) error
}

func scanEvent(scanner eventScanner) (Event, error) {
	var event Event
	var status string
	var createdRaw string
	var updatedRaw string
	if err := scanner.Scan(
		&event.EventID,
		&status,
		&event.Board,
		&event.ThreadID,
		&event.PostID,
		&event.ErrorCode,
		&event.ErrorMessage,
		&createdRaw,
		&updatedRaw,
	); err != nil {
		if err == sql.ErrNoRows {
			return Event{}, err
		}
		return Event{}, fmt.Errorf("scan channer event: %w", err)
	}

	event.Status = EventStatus(status)
	var err error
	event.CreatedAt, err = storage.ParseTime(createdRaw)
	if err != nil {
		return Event{}, fmt.Errorf("parse created_at: %w", err)
	}
	event.UpdatedAt, err = storage.ParseTime(updatedRaw)
	if err != nil {
		return Event{}, fmt.Errorf("parse updated_at: %w", err)
	}
	return event, nil
}
