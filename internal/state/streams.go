package state

import (
	"context"
	"database/sql"
	"fmt"
)

type StreamState struct {
	Key             string
	Active          bool
	LiveNotified    bool
	Consecutive404s int
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
