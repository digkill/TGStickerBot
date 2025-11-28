package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/digkill/TGStickerBot/internal/models"
)

type PaymentRepository struct {
	db *sql.DB
}

func NewPaymentRepository(db *sql.DB) *PaymentRepository {
	return &PaymentRepository{db: db}
}

func (r *PaymentRepository) Create(ctx context.Context, payment *models.Payment) error {
	const query = `
INSERT INTO payments (user_id, plan_id, provider, provider_payment_charge_id, currency, amount, status, raw_payload)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	res, err := r.db.ExecContext(ctx, query, payment.UserID, payment.PlanID, payment.Provider, payment.ProviderCharge, payment.Currency, payment.Amount, payment.Status, payment.RawPayload)
	if err != nil {
		return fmt.Errorf("insert payment: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("last insert id: %w", err)
	}
	payment.ID = id
	return nil
}

func (r *PaymentRepository) UpdateStatus(ctx context.Context, paymentID int64, status string, payload string) error {
	const query = `UPDATE payments SET status = ?, raw_payload = ?, updated_at = NOW() WHERE id = ?`
	if _, err := r.db.ExecContext(ctx, query, status, payload, paymentID); err != nil {
		return fmt.Errorf("update payment status: %w", err)
	}
	return nil
}

func (r *PaymentRepository) FindByProviderCharge(ctx context.Context, provider, chargeID string) (*models.Payment, error) {
	const query = `
SELECT id, user_id, plan_id, provider, provider_payment_charge_id, currency, amount, status, raw_payload, created_at, COALESCE(updated_at, created_at) as updated_at
FROM payments WHERE provider = ? AND provider_payment_charge_id = ? LIMIT 1`
	row := r.db.QueryRowContext(ctx, query, provider, chargeID)
	var p models.Payment
	var planID sql.NullInt64
	if err := row.Scan(&p.ID, &p.UserID, &planID, &p.Provider, &p.ProviderCharge, &p.Currency, &p.Amount, &p.Status, &p.RawPayload, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan payment: %w", err)
	}
	if planID.Valid {
		p.PlanID = &planID.Int64
	}
	return &p, nil
}
