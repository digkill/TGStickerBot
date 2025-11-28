package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/example/stickerbot/internal/config"
	"github.com/example/stickerbot/internal/models"
	"github.com/example/stickerbot/internal/repository"
)

type PaymentService struct {
	cfg      config.Config
	log      *slog.Logger
	payments *repository.PaymentRepository
	users    *repository.UserRepository
	plan     CreditPlan
}

type CreditPlan struct {
	ID          string
	Title       string
	Description string
	Currency    string
	PriceMinor  int
	Credits     int
}

func NewPaymentService(cfg config.Config, log *slog.Logger, payments *repository.PaymentRepository, users *repository.UserRepository) *PaymentService {
	plan := CreditPlan{
		ID:          "default_credits",
		Title:       "Пакет генераций",
		Description: "Дополнительные генерации изображений",
		Currency:    cfg.PaymentCurrency,
		PriceMinor:  cfg.PaymentPriceMinorUnits,
		Credits:     cfg.PaymentCreditsPerPackage,
	}
	return &PaymentService{
		cfg:      cfg,
		log:      log,
		payments: payments,
		users:    users,
		plan:     plan,
	}
}

func (s *PaymentService) SendInvoice(bot *tgbotapi.BotAPI, chatID int64) error {
	prices := []tgbotapi.LabeledPrice{
		{
			Label:  fmt.Sprintf("%d генераций", s.plan.Credits),
			Amount: s.plan.PriceMinor,
		},
	}

	payload, _ := json.Marshal(map[string]any{
		"plan_id": s.plan.ID,
		"credits": s.plan.Credits,
	})

	invoice := tgbotapi.NewInvoice(chatID,
		s.plan.Title,
		s.plan.Description,
		string(payload),
		s.cfg.TelegramPaymentProviderToken,
		"topup",
		s.plan.Currency,
		prices,
	)

	if _, err := bot.Send(invoice); err != nil {
		return fmt.Errorf("send invoice: %w", err)
	}
	return nil
}

func (s *PaymentService) HandlePreCheckout(bot *tgbotapi.BotAPI, query *tgbotapi.PreCheckoutQuery) error {
	response := tgbotapi.PreCheckoutConfig{
		PreCheckoutQueryID: query.ID,
		OK:                 true,
	}
	if _, err := bot.Request(response); err != nil {
		return fmt.Errorf("answer pre-checkout: %w", err)
	}
	return nil
}

func (s *PaymentService) HandleSuccessfulPayment(ctx context.Context, user *models.User, payment *tgbotapi.SuccessfulPayment) error {
	var payload struct {
		PlanID  string `json:"plan_id"`
		Credits int    `json:"credits"`
	}
	if err := json.Unmarshal([]byte(payment.InvoicePayload), &payload); err != nil {
		return fmt.Errorf("parse payment payload: %w", err)
	}
	if payload.Credits <= 0 {
		payload.Credits = s.plan.Credits
	}

	if err := s.users.UpdatePaidCredits(ctx, user.ID, payload.Credits); err != nil {
		return fmt.Errorf("add paid credits: %w", err)
	}

	record := &models.Payment{
		UserID:         user.ID,
		Provider:       "telegram",
		ProviderCharge: payment.ProviderPaymentChargeID,
		Currency:       payment.Currency,
		Amount:         payment.TotalAmount,
		Status:         "paid",
		RawPayload:     string(jsonMustMarshal(payment)),
	}
	if err := s.payments.Create(ctx, record); err != nil {
		return fmt.Errorf("record payment: %w", err)
	}

	s.log.Info("payment processed", "user_id", user.ID, "credits", payload.Credits)
	return nil
}

func jsonMustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}
