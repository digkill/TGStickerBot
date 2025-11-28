package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/example/stickerbot/internal/repository"
)

var ErrPromoInvalid = errors.New("promo code invalid")
var ErrPromoAlreadyRedeemed = errors.New("promo code already redeemed")

type PromoService struct {
	promos *repository.PromoRepository
	users  *repository.UserRepository
}

func NewPromoService(promos *repository.PromoRepository, users *repository.UserRepository) *PromoService {
	return &PromoService{promos: promos, users: users}
}

func (s *PromoService) Apply(ctx context.Context, userID int64, code string, bonus int) error {
	promo, err := s.promos.GetByCode(ctx, code)
	if err != nil {
		return fmt.Errorf("get promo: %w", err)
	}
	if promo == nil {
		return ErrPromoInvalid
	}

	tx, err := s.promos.DB().BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var uses, maxUses int
	row := tx.QueryRowContext(ctx, `SELECT uses, max_uses FROM promo_codes WHERE id = ? FOR UPDATE`, promo.ID)
	if err := row.Scan(&uses, &maxUses); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrPromoInvalid
		}
		return fmt.Errorf("lock promo: %w", err)
	}
	if uses >= maxUses {
		return fmt.Errorf("promo code exhausted")
	}

	row = tx.QueryRowContext(ctx, `SELECT 1 FROM promo_redemptions WHERE user_id = ? AND promo_code_id = ?`, userID, promo.ID)
	var dummy int
	if err := row.Scan(&dummy); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check redemption: %w", err)
		}
	} else {
		return ErrPromoAlreadyRedeemed
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO promo_redemptions (user_id, promo_code_id) VALUES (?, ?)`, userID, promo.ID); err != nil {
		return fmt.Errorf("insert redemption: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `UPDATE promo_codes SET uses = uses + 1 WHERE id = ?`, promo.ID); err != nil {
		return fmt.Errorf("increment promo uses: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `UPDATE users SET promo_credits = promo_credits + ?, updated_at = NOW() WHERE id = ?`, bonus, userID); err != nil {
		return fmt.Errorf("add promo credits: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit promo tx: %w", err)
	}

	return nil
}
