package db

import (
	"context"
	"database/sql"
	"fmt"
)

type Statement struct {
	SQL          string
	ErrorContext string
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func ExecStatements(ctx context.Context, conn execer, statements ...Statement) error {
	for _, statement := range statements {
		if _, err := conn.ExecContext(ctx, statement.SQL); err != nil {
			return fmt.Errorf("%s: %w", statement.ErrorContext, err)
		}
	}

	return nil
}
