package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/example/stickerbot/internal/models"
)

type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) DB() *sql.DB {
	return r.db
}

func (r *UserRepository) FindByTelegramID(ctx context.Context, telegramID int64) (*models.User, error) {
	const query = `
SELECT id, telegram_id, COALESCE(username, ''), COALESCE(first_name, ''), COALESCE(last_name, ''), free_daily_limit, promo_credits, paid_credits, created_at, updated_at
FROM users WHERE telegram_id = ?`
	row := r.db.QueryRowContext(ctx, query, telegramID)
	var u models.User
	if err := row.Scan(&u.ID, &u.TelegramID, &u.Username, &u.FirstName, &u.LastName, &u.FreeDailyLimit, &u.PromoCredits, &u.PaidCredits, &u.CreatedAt, &u.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan user: %w", err)
	}
	return &u, nil
}

func (r *UserRepository) Create(ctx context.Context, user *models.User) (*models.User, error) {
	const query = `
INSERT INTO users (telegram_id, username, first_name, last_name, free_daily_limit, promo_credits, paid_credits)
VALUES (?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?)`
	res, err := r.db.ExecContext(ctx, query, user.TelegramID, user.Username, user.FirstName, user.LastName, user.FreeDailyLimit, user.PromoCredits, user.PaidCredits)
	if err != nil {
		return nil, fmt.Errorf("insert user: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}
	user.ID = id
	return user, nil
}

func (r *UserRepository) UpdateProfile(ctx context.Context, userID int64, username, firstName, lastName string) error {
	const query = `
UPDATE users SET username = NULLIF(?, ''), first_name = NULLIF(?, ''), last_name = NULLIF(?, ''), updated_at = NOW()
WHERE id = ?`
	if _, err := r.db.ExecContext(ctx, query, username, firstName, lastName, userID); err != nil {
		return fmt.Errorf("update profile: %w", err)
	}
	return nil
}

func (r *UserRepository) Ensure(ctx context.Context, telegramID int64, username, firstName, lastName string, freeLimit int) (*models.User, error) {
	user, err := r.FindByTelegramID(ctx, telegramID)
	if err != nil {
		return nil, err
	}
	if user != nil {
		go func() {
			_ = r.UpdateProfile(context.Background(), user.ID, username, firstName, lastName)
		}()
		return user, nil
	}
	newUser := &models.User{
		TelegramID:     telegramID,
		Username:       username,
		FirstName:      firstName,
		LastName:       lastName,
		FreeDailyLimit: freeLimit,
	}
	return r.Create(ctx, newUser)
}

func (r *UserRepository) UpdatePromoCredits(ctx context.Context, userID int64, delta int) error {
	const query = `UPDATE users SET promo_credits = GREATEST(promo_credits + ?, 0), updated_at = NOW() WHERE id = ?`
	if _, err := r.db.ExecContext(ctx, query, delta, userID); err != nil {
		return fmt.Errorf("update promo credits: %w", err)
	}
	return nil
}

func (r *UserRepository) UpdatePaidCredits(ctx context.Context, userID int64, delta int) error {
	const query = `UPDATE users SET paid_credits = GREATEST(paid_credits + ?, 0), updated_at = NOW() WHERE id = ?`
	if _, err := r.db.ExecContext(ctx, query, delta, userID); err != nil {
		return fmt.Errorf("update paid credits: %w", err)
	}
	return nil
}

func (r *UserRepository) ConsumePromoCredit(ctx context.Context, userID int64) (bool, error) {
	const query = `
UPDATE users SET promo_credits = promo_credits - 1, updated_at = NOW()
WHERE id = ? AND promo_credits > 0`
	res, err := r.db.ExecContext(ctx, query, userID)
	if err != nil {
		return false, fmt.Errorf("consume promo credit: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("promo rows affected: %w", err)
	}
	return affected > 0, nil
}

func (r *UserRepository) ConsumePaidCredit(ctx context.Context, userID int64) (bool, error) {
	const query = `
UPDATE users SET paid_credits = paid_credits - 1, updated_at = NOW()
WHERE id = ? AND paid_credits > 0`
	res, err := r.db.ExecContext(ctx, query, userID)
	if err != nil {
		return false, fmt.Errorf("consume paid credit: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("paid rows affected: %w", err)
	}
	return affected > 0, nil
}

func (r *UserRepository) ListTelegramIDs(ctx context.Context) ([]int64, error) {
	const query = `SELECT telegram_id FROM users`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list telegram ids: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan telegram id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
