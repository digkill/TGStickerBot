package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/digkill/TGStickerBot/internal/config"
	"github.com/digkill/TGStickerBot/internal/kie"
	"github.com/digkill/TGStickerBot/internal/models"
	"github.com/digkill/TGStickerBot/internal/repository"
)

var ErrCreditsRequired = errors.New("insufficient credits, payment required")

const creditsPerGeneration = 5

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

	cost := models.CostTypePromo
	switch {
	case user.PromoCredits >= creditsPerGeneration:
		cost = models.CostTypePromo
	case user.PaidCredits >= creditsPerGeneration:
		cost = models.CostTypePaid
	default:
		return nil, ErrCreditsRequired
	}

	opts := kie.GenerateOptions{
		Prompt:       req.Prompt,
		AspectRatio:  req.AspectRatio,
		Resolution:   req.Resolution,
		InputURLs:    req.InputURLs,
		OutputFormat: req.OutputFormat,
	}

	start := time.Now()
	s.log.Info("generation started",
		"user_id", user.ID,
		"model", req.Model,
		"prompt_len", len(req.Prompt),
		"references", len(req.InputURLs),
	)

	var image *kie.Image
	var err error
	switch req.Model {
	case models.ModelFlux2:
		image, err = s.kie.GenerateFlux2(ctx, opts)
	case models.ModelNanoBanana:
		image, err = s.kie.GenerateNanoBanana(ctx, opts)
	default:
		return nil, fmt.Errorf("unsupported model: %s", req.Model)
	}
	if err != nil {
		s.log.Error("generation request failed",
			"user_id", user.ID,
			"model", req.Model,
			"err", err,
		)
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
		user.PromoCredits -= creditsPerGeneration
	case models.CostTypePaid:
		ok, consumeErr := s.users.ConsumePaidCredit(ctx, user.ID)
		if consumeErr != nil {
			return nil, consumeErr
		}
		if !ok {
			return nil, ErrCreditsRequired
		}
		user.PaidCredits -= creditsPerGeneration
	}

	if err := s.generations.Log(ctx, user.ID, req.Model, req.Prompt, cost); err != nil {
		s.log.Error("failed to log generation",
			"user_id", user.ID,
			"model", req.Model,
			"err", err,
		)
	}

	s.log.Info("generation completed",
		"user_id", user.ID,
		"model", req.Model,
		"cost_type", cost,
		"duration_ms", time.Since(start).Milliseconds(),
		"has_url", image.URL != "",
		"bytes", len(image.Bytes),
	)

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
