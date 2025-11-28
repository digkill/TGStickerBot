package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/example/stickerbot/internal/models"
)

type PromoRepository struct {
	db *sql.DB
}

func NewPromoRepository(db *sql.DB) *PromoRepository {
	return &PromoRepository{db: db}
}

func (r *PromoRepository) DB() *sql.DB {
	return r.db
}

func (r *PromoRepository) GetByCode(ctx context.Context, code string) (*models.PromoCode, error) {
	const query = `SELECT id, code, max_uses, uses FROM promo_codes WHERE code = ?`
	row := r.db.QueryRowContext(ctx, query, code)
	var promo models.PromoCode
	if err := row.Scan(&promo.ID, &promo.Code, &promo.MaxUses, &promo.Uses); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan promo: %w", err)
	}
	return &promo, nil
}

func (r *PromoRepository) IncrementUsage(ctx context.Context, promoID int64) error {
	const query = `
UPDATE promo_codes SET uses = uses + 1
WHERE id = ? AND uses < max_uses`
	res, err := r.db.ExecContext(ctx, query, promoID)
	if err != nil {
		return fmt.Errorf("increment promo usage: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("promo usage rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("promo code exhausted")
	}
	return nil
}

func (r *PromoRepository) HasUserRedeemed(ctx context.Context, userID, promoID int64) (bool, error) {
	const query = `SELECT 1 FROM promo_redemptions WHERE user_id = ? AND promo_code_id = ?`
	row := r.db.QueryRowContext(ctx, query, userID, promoID)
	var dummy int
	if err := row.Scan(&dummy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("check promo redemption: %w", err)
	}
	return true, nil
}

func (r *PromoRepository) RecordRedemption(ctx context.Context, userID, promoID int64) error {
	const query = `
INSERT INTO promo_redemptions (user_id, promo_code_id)
VALUES (?, ?)`
	if _, err := r.db.ExecContext(ctx, query, userID, promoID); err != nil {
		return fmt.Errorf("record redemption: %w", err)
	}
	return nil
}
