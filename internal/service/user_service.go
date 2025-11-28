package service

import (
	"context"
	"fmt"

	"github.com/example/stickerbot/internal/models"
	"github.com/example/stickerbot/internal/repository"
)

type UserService struct {
	users *repository.UserRepository
}

func NewUserService(users *repository.UserRepository) *UserService {
	return &UserService{users: users}
}

func (s *UserService) Ensure(ctx context.Context, telegramID int64, username, firstName, lastName string, freeLimit int) (*models.User, error) {
	user, err := s.users.Ensure(ctx, telegramID, username, firstName, lastName, freeLimit)
	if err != nil {
		return nil, fmt.Errorf("ensure user: %w", err)
	}
	return user, nil
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
