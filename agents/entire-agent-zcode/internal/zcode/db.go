package zcode

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// DBQuerier runs read-only queries against ZCode's SQLite store. The default
// implementation shells out to the `sqlite3` CLI (opened read-only so the
// app's WAL is never touched); tests substitute their own querier.
type DBQuerier interface {
	Query(ctx context.Context, dbPath, query string) ([]byte, error)
}

type SQLiteQuerier struct{}

func (q *SQLiteQuerier) Query(ctx context.Context, dbPath, query string) ([]byte, error) {
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("empty query")
	}
	cmd := exec.CommandContext(ctx, "sqlite3", "-readonly", "-json", dbPath, query)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("sqlite3 query: %w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("sqlite3 query: %w", err)
	}
	return out, nil
}

// sqlLiteral single-quotes a value for embedding in a query. sqlite3 has no
// bind parameters on the CLI, so session ids must be escaped in-band.
func sqlLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
