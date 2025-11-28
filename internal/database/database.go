package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"

	"github.com/digkill/TGStickerBot/internal/config"
)

// Connect opens the MySQL connection with sensible pooling defaults.
func Connect(cfg config.Config) (*sql.DB, error) {
	db, err := sql.Open("mysql", cfg.MySQLDSN)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}

	db.SetConnMaxLifetime(time.Minute * 5)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}

	return db, nil
}

// Migrate runs the bootstrap schema to ensure required tables exist.
func Migrate(ctx context.Context, db *sql.DB) error {
	stmts := strings.Split(schema, ";")
	for _, stmt := range stmts {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("apply schema: %w", err)
		}
	}

	optional := []struct {
		stmt          string
		allowedErrors []uint16
	}{
		{
			stmt:          `ALTER TABLE users ADD COLUMN subscription_bonus_granted TINYINT(1) NOT NULL DEFAULT 0 AFTER paid_credits`,
			allowedErrors: []uint16{1060},
		},
		{
			stmt:          `ALTER TABLE promo_codes ADD COLUMN created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP`,
			allowedErrors: []uint16{1060},
		},
		{
			stmt:          `ALTER TABLE payments ADD COLUMN plan_id BIGINT NULL AFTER user_id`,
			allowedErrors: []uint16{1060},
		},
	}

	for _, opt := range optional {
		if err := execIgnoreErrors(ctx, db, opt.stmt, opt.allowedErrors...); err != nil {
			return fmt.Errorf("apply optional schema: %w", err)
		}
	}

	return nil
}

func execIgnoreErrors(ctx context.Context, db *sql.DB, stmt string, allowedCodes ...uint16) error {
	if stmt == "" {
		return nil
	}
	_, err := db.ExecContext(ctx, stmt)
	if err == nil {
		return nil
	}
	var mysqlErr *mysqlDriver.MySQLError
	if errors.As(err, &mysqlErr) {
		for _, code := range allowedCodes {
			if mysqlErr.Number == code {
				return nil
			}
		}
	}
	return err
}
