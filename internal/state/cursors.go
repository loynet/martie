package state

import (
	"context"
	"database/sql"
	"fmt"
)

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
