package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/digkill/TGStickerBot/internal/models"
)

type PlanRepository struct {
	db *sql.DB
}

func NewPlanRepository(db *sql.DB) *PlanRepository {
	return &PlanRepository{db: db}
}

func (r *PlanRepository) List(ctx context.Context) ([]models.Plan, error) {
	const query = `
SELECT id, title, COALESCE(description, ''), currency, price_minor_units, credits, is_active, created_at, updated_at
FROM pricing_plans
ORDER BY id ASC`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	defer rows.Close()

	var plans []models.Plan
	for rows.Next() {
		var plan models.Plan
		if err := rows.Scan(&plan.ID, &plan.Title, &plan.Description, &plan.Currency, &plan.PriceMinorUnits, &plan.Credits, &plan.IsActive, &plan.CreatedAt, &plan.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan plan: %w", err)
		}
		plans = append(plans, plan)
	}
	return plans, rows.Err()
}

func (r *PlanRepository) GetDefault(ctx context.Context) (*models.Plan, error) {
	const query = `
SELECT id, title, COALESCE(description, ''), currency, price_minor_units, credits, is_active, created_at, updated_at
FROM pricing_plans
WHERE is_active = 1
ORDER BY id ASC
LIMIT 1`
	row := r.db.QueryRowContext(ctx, query)
	var plan models.Plan
	if err := row.Scan(&plan.ID, &plan.Title, &plan.Description, &plan.Currency, &plan.PriceMinorUnits, &plan.Credits, &plan.IsActive, &plan.CreatedAt, &plan.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get default plan: %w", err)
	}
	return &plan, nil
}

func (r *PlanRepository) GetByID(ctx context.Context, id int64) (*models.Plan, error) {
	const query = `
SELECT id, title, COALESCE(description, ''), currency, price_minor_units, credits, is_active, created_at, updated_at
FROM pricing_plans
WHERE id = ?`
	row := r.db.QueryRowContext(ctx, query, id)
	var plan models.Plan
	if err := row.Scan(&plan.ID, &plan.Title, &plan.Description, &plan.Currency, &plan.PriceMinorUnits, &plan.Credits, &plan.IsActive, &plan.CreatedAt, &plan.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get plan: %w", err)
	}
	return &plan, nil
}

func (r *PlanRepository) Create(ctx context.Context, plan *models.Plan) (*models.Plan, error) {
	const query = `
INSERT INTO pricing_plans (title, description, currency, price_minor_units, credits, is_active)
VALUES (?, NULLIF(?, ''), ?, ?, ?, ?)`
	res, err := r.db.ExecContext(ctx, query, plan.Title, plan.Description, plan.Currency, plan.PriceMinorUnits, plan.Credits, plan.IsActive)
	if err != nil {
		return nil, fmt.Errorf("create plan: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("plan last insert id: %w", err)
	}
	plan.ID = id
	created, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (r *PlanRepository) Update(ctx context.Context, plan *models.Plan) (*models.Plan, error) {
	const query = `
UPDATE pricing_plans
SET title = ?, description = NULLIF(?, ''), currency = ?, price_minor_units = ?, credits = ?, is_active = ?, updated_at = NOW()
WHERE id = ?`
	if _, err := r.db.ExecContext(ctx, query, plan.Title, plan.Description, plan.Currency, plan.PriceMinorUnits, plan.Credits, plan.IsActive, plan.ID); err != nil {
		return nil, fmt.Errorf("update plan: %w", err)
	}
	return r.GetByID(ctx, plan.ID)
}

func (r *PlanRepository) Delete(ctx context.Context, id int64) error {
	const query = `DELETE FROM pricing_plans WHERE id = ?`
	if _, err := r.db.ExecContext(ctx, query, id); err != nil {
		return fmt.Errorf("delete plan: %w", err)
	}
	return nil
}
