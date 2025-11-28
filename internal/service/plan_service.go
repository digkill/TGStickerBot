package service

import (
	"context"
	"fmt"

	"github.com/digkill/TGStickerBot/internal/config"
	"github.com/digkill/TGStickerBot/internal/models"
	"github.com/digkill/TGStickerBot/internal/repository"
)

type PlanService struct {
	cfg  config.Config
	repo *repository.PlanRepository
}

type CreatePlanInput struct {
	Title           string
	Description     string
	Currency        string
	PriceMinorUnits int
	Credits         int
	IsActive        *bool
}

type UpdatePlanInput struct {
	Title           *string
	Description     *string
	Currency        *string
	PriceMinorUnits *int
	Credits         *int
	IsActive        *bool
}

func NewPlanService(cfg config.Config, repo *repository.PlanRepository) *PlanService {
	return &PlanService{cfg: cfg, repo: repo}
}

func (s *PlanService) EnsureDefaultPlan(ctx context.Context) error {
	plan, err := s.repo.GetDefault(ctx)
	if err != nil {
		return err
	}
	if plan != nil {
		return nil
	}
	defaultPlan := &models.Plan{
		Title:           "Пакет генераций",
		Description:     "Базовый пакет генераций",
		Currency:        s.cfg.PaymentCurrency,
		PriceMinorUnits: s.cfg.PaymentPriceMinorUnits,
		Credits:         s.cfg.PaymentCreditsPerPackage,
		IsActive:        true,
	}
	if _, err := s.repo.Create(ctx, defaultPlan); err != nil {
		return fmt.Errorf("create default plan: %w", err)
	}
	return nil
}

func (s *PlanService) List(ctx context.Context) ([]models.Plan, error) {
	return s.repo.List(ctx)
}

func (s *PlanService) Create(ctx context.Context, input CreatePlanInput) (*models.Plan, error) {
	if input.Title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if input.Currency == "" {
		input.Currency = s.cfg.PaymentCurrency
	}
	if input.PriceMinorUnits <= 0 {
		return nil, fmt.Errorf("price must be positive")
	}
	if input.Credits <= 0 {
		return nil, fmt.Errorf("credits must be positive")
	}
	isActive := true
	if input.IsActive != nil {
		isActive = *input.IsActive
	}
	plan := models.Plan{
		Title:           input.Title,
		Description:     input.Description,
		Currency:        input.Currency,
		PriceMinorUnits: input.PriceMinorUnits,
		Credits:         input.Credits,
		IsActive:        isActive,
	}
	return s.repo.Create(ctx, &plan)
}

func (s *PlanService) Update(ctx context.Context, id int64, input UpdatePlanInput) (*models.Plan, error) {
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("plan not found")
	}
	if input.Title != nil {
		existing.Title = *input.Title
	}
	if input.Description != nil {
		existing.Description = *input.Description
	}
	if input.Currency != nil && *input.Currency != "" {
		existing.Currency = *input.Currency
	}
	if input.PriceMinorUnits != nil && *input.PriceMinorUnits > 0 {
		existing.PriceMinorUnits = *input.PriceMinorUnits
	}
	if input.Credits != nil && *input.Credits > 0 {
		existing.Credits = *input.Credits
	}
	if input.IsActive != nil {
		existing.IsActive = *input.IsActive
	}
	return s.repo.Update(ctx, existing)
}

func (s *PlanService) Delete(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
}

func (s *PlanService) GetDefault(ctx context.Context) (*models.Plan, error) {
	return s.repo.GetDefault(ctx)
}

func (s *PlanService) GetByID(ctx context.Context, id int64) (*models.Plan, error) {
	return s.repo.GetByID(ctx, id)
}
