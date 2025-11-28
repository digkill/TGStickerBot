package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/example/stickerbot/internal/config"
	"github.com/example/stickerbot/internal/kie"
	"github.com/example/stickerbot/internal/models"
	"github.com/example/stickerbot/internal/repository"
)

var ErrCreditsRequired = errors.New("insufficient credits, payment required")

type GenerationService struct {
	cfg         config.Config
	log         *slog.Logger
	users       *repository.UserRepository
	generations *repository.GenerationRepository
	kie         *kie.Client
}

type GenerationRequest struct {
	Model        models.ModelType
	Prompt       string
	AspectRatio  string
	Resolution   string
	InputURLs    []string
	OutputFormat string
}

type GenerationResult struct {
	Image  *kie.Image
	Cost   models.CostType
	Prompt string
	Model  models.ModelType
}

func NewGenerationService(cfg config.Config, log *slog.Logger, users *repository.UserRepository, generations *repository.GenerationRepository, client *kie.Client) *GenerationService {
	return &GenerationService{
		cfg:         cfg,
		log:         log,
		users:       users,
		generations: generations,
		kie:         client,
	}
}

func (s *GenerationService) Generate(ctx context.Context, user *models.User, req GenerationRequest) (*GenerationResult, error) {
	if req.Prompt == "" {
		return nil, fmt.Errorf("prompt cannot be empty")
	}
	if req.AspectRatio == "" {
		req.AspectRatio = "1:1"
	}
	if req.Resolution == "" {
		req.Resolution = "1K"
	}

	todayCount, err := s.generations.CountForDay(ctx, user.ID, time.Now().UTC())
	if err != nil {
		return nil, err
	}

	cost := models.CostTypeFree
	if todayCount >= user.FreeDailyLimit {
		switch {
		case user.PromoCredits > 0:
			cost = models.CostTypePromo
		case user.PaidCredits > 0:
			cost = models.CostTypePaid
		default:
			return nil, ErrCreditsRequired
		}
	}

	opts := kie.GenerateOptions{
		Prompt:       req.Prompt,
		AspectRatio:  req.AspectRatio,
		Resolution:   req.Resolution,
		InputURLs:    req.InputURLs,
		OutputFormat: req.OutputFormat,
	}

	var image *kie.Image
	switch req.Model {
	case models.ModelFlux2:
		image, err = s.kie.GenerateFlux2(ctx, opts)
	case models.ModelNanoBanana:
		image, err = s.kie.GenerateNanoBanana(ctx, opts)
	default:
		return nil, fmt.Errorf("unsupported model: %s", req.Model)
	}
	if err != nil {
		return nil, err
	}

	switch cost {
	case models.CostTypePromo:
		ok, consumeErr := s.users.ConsumePromoCredit(ctx, user.ID)
		if consumeErr != nil {
			return nil, consumeErr
		}
		if !ok {
			return nil, ErrCreditsRequired
		}
		if user.PromoCredits > 0 {
			user.PromoCredits--
		}
	case models.CostTypePaid:
		ok, consumeErr := s.users.ConsumePaidCredit(ctx, user.ID)
		if consumeErr != nil {
			return nil, consumeErr
		}
		if !ok {
			return nil, ErrCreditsRequired
		}
		if user.PaidCredits > 0 {
			user.PaidCredits--
		}
	}

	if err := s.generations.Log(ctx, user.ID, req.Model, req.Prompt, cost); err != nil {
		s.log.Error("failed to log generation", "err", err)
	}

	return &GenerationResult{
		Image:  image,
		Cost:   cost,
		Prompt: req.Prompt,
		Model:  req.Model,
	}, nil
}

func (s *GenerationService) DailyCount(ctx context.Context, userID int64) (int, error) {
	return s.generations.CountForDay(ctx, userID, time.Now().UTC())
}
