package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/digkill/TGStickerBot/internal/models"
)

type GenerationRepository struct {
	db *sql.DB
}

func NewGenerationRepository(db *sql.DB) *GenerationRepository {
	return &GenerationRepository{db: db}
}

func (r *GenerationRepository) Log(ctx context.Context, userID int64, model models.ModelType, prompt string, cost models.CostType) error {
	const query = `
INSERT INTO generation_logs (user_id, model, prompt, cost_type)
VALUES (?, ?, ?, ?)`
	if _, err := r.db.ExecContext(ctx, query, userID, model, prompt, cost); err != nil {
		return fmt.Errorf("insert generation log: %w", err)
	}
	return nil
}

func (r *GenerationRepository) CountForDay(ctx context.Context, userID int64, day time.Time) (int, error) {
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	const query = `
SELECT COUNT(*) FROM generation_logs
WHERE user_id = ? AND created_at >= ? AND created_at < ?`
	row := r.db.QueryRowContext(ctx, query, userID, start, end)
	var count int
	if err := row.Scan(&count); err != nil {
		return 0, fmt.Errorf("count daily generations: %w", err)
	}
	return count, nil
}
