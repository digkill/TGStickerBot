package service

import (
	"context"
	"fmt"

	"github.com/digkill/TGStickerBot/internal/models"
	"github.com/digkill/TGStickerBot/internal/repository"
)

type UserService struct {
	users *repository.UserRepository
}

func NewUserService(users *repository.UserRepository) *UserService {
	return &UserService{users: users}
}

func (s *UserService) Ensure(ctx context.Context, telegramID int64, username, firstName, lastName string, freeLimit int) (*models.User, bool, error) {
	user, created, err := s.users.Ensure(ctx, telegramID, username, firstName, lastName, freeLimit)
	if err != nil {
		return nil, false, fmt.Errorf("ensure user: %w", err)
	}
	return user, created, nil
}

func (s *UserService) UpdatePromoCredits(ctx context.Context, userID int64, delta int) error {
	return s.users.UpdatePromoCredits(ctx, userID, delta)
}

func (s *UserService) UpdatePaidCredits(ctx context.Context, userID int64, delta int) error {
	return s.users.UpdatePaidCredits(ctx, userID, delta)
}

func (s *UserService) ListTelegramIDs(ctx context.Context) ([]int64, error) {
	ids, err := s.users.ListTelegramIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list telegram ids: %w", err)
	}
	return ids, nil
}

func (s *UserService) SetSubscriptionBonusGranted(ctx context.Context, userID int64, granted bool) error {
	return s.users.SetSubscriptionBonusGranted(ctx, userID, granted)
}
