package state

import (
	"context"
	"database/sql"
	"fmt"

	"martie/internal/storage"
)

type Store struct {
	db *storage.DB
}

type Stream struct {
	Key             string
	Active          bool
	LiveNotified    bool
	Consecutive404s int
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
CREATE TABLE IF NOT EXISTS streamnotifier_streams (
  stream_key TEXT PRIMARY KEY,
  active INTEGER NOT NULL,
  live_notified INTEGER NOT NULL,
  consecutive_404s INTEGER NOT NULL
);
`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("init streamnotifier schema: %w", err)
	}
	return nil
}

func (s *Store) GetStreamState(ctx context.Context, key string) (Stream, bool, error) {
	const query = `
SELECT stream_key, active, live_notified, consecutive_404s
FROM streamnotifier_streams
WHERE stream_key = ?;
`

	var stream Stream
	var active int
	var liveNotified int

	err := s.db.QueryRowContext(ctx, query, key).Scan(
		&stream.Key,
		&active,
		&liveNotified,
		&stream.Consecutive404s,
	)
	if err == sql.ErrNoRows {
		return Stream{}, false, nil
	}
	if err != nil {
		return Stream{}, false, fmt.Errorf("query stream state: %w", err)
	}

	stream.Active = active != 0
	stream.LiveNotified = liveNotified != 0
	return stream, true, nil
}

func (s *Store) UpsertStreamState(ctx context.Context, stream Stream) error {
	const statement = `
INSERT INTO streamnotifier_streams (stream_key, active, live_notified, consecutive_404s)
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
		storage.BoolToInt(stream.Active),
		storage.BoolToInt(stream.LiveNotified),
		stream.Consecutive404s,
	)
	if err != nil {
		return fmt.Errorf("upsert stream state: %w", err)
	}

	return nil
}
