package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/digkill/TGStickerBot/internal/models"
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
	const query = `SELECT id, code, max_uses, uses, created_at FROM promo_codes WHERE code = ?`
	row := r.db.QueryRowContext(ctx, query, code)
	var promo models.PromoCode
	if err := row.Scan(&promo.ID, &promo.Code, &promo.MaxUses, &promo.Uses, &promo.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan promo: %w", err)
	}
	return &promo, nil
}

func (r *PromoRepository) GetByID(ctx context.Context, id int64) (*models.PromoCode, error) {
	const query = `SELECT id, code, max_uses, uses, created_at FROM promo_codes WHERE id = ?`
	row := r.db.QueryRowContext(ctx, query, id)
	var promo models.PromoCode
	if err := row.Scan(&promo.ID, &promo.Code, &promo.MaxUses, &promo.Uses, &promo.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get promo by id: %w", err)
	}
	return &promo, nil
}

func (r *PromoRepository) List(ctx context.Context) ([]models.PromoCode, error) {
	const query = `SELECT id, code, max_uses, uses, created_at FROM promo_codes ORDER BY id DESC`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list promos: %w", err)
	}
	defer rows.Close()

	var promos []models.PromoCode
	for rows.Next() {
		var promo models.PromoCode
		if err := rows.Scan(&promo.ID, &promo.Code, &promo.MaxUses, &promo.Uses, &promo.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan promo list: %w", err)
		}
		promos = append(promos, promo)
	}
	return promos, rows.Err()
}

func (r *PromoRepository) Create(ctx context.Context, promo *models.PromoCode) (*models.PromoCode, error) {
	const query = `
INSERT INTO promo_codes (code, max_uses, uses)
VALUES (?, ?, 0)`
	res, err := r.db.ExecContext(ctx, query, promo.Code, promo.MaxUses)
	if err != nil {
		return nil, fmt.Errorf("create promo: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("promo last insert id: %w", err)
	}
	return r.GetByID(ctx, id)
}

func (r *PromoRepository) Update(ctx context.Context, promo *models.PromoCode) (*models.PromoCode, error) {
	const query = `
UPDATE promo_codes
SET code = ?, max_uses = ?, uses = ?
WHERE id = ?`
	if _, err := r.db.ExecContext(ctx, query, promo.Code, promo.MaxUses, promo.Uses, promo.ID); err != nil {
		return nil, fmt.Errorf("update promo: %w", err)
	}
	return r.GetByID(ctx, promo.ID)
}

func (r *PromoRepository) Delete(ctx context.Context, id int64) error {
	const query = `DELETE FROM promo_codes WHERE id = ?`
	if _, err := r.db.ExecContext(ctx, query, id); err != nil {
		return fmt.Errorf("delete promo: %w", err)
	}
	return nil
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
