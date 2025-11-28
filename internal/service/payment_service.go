package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/digkill/TGStickerBot/internal/config"
	"github.com/digkill/TGStickerBot/internal/models"
	"github.com/digkill/TGStickerBot/internal/repository"
)

type PaymentService struct {
	cfg      config.Config
	payments *repository.PaymentRepository
	users    *repository.UserRepository
	plans    *PlanService
	client   *http.Client
}

func NewPaymentService(cfg config.Config, payments *repository.PaymentRepository, users *repository.UserRepository, plans *PlanService) *PaymentService {
	return &PaymentService{
		cfg:      cfg,
		payments: payments,
		users:    users,
		plans:    plans,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SendInvoice sends payment link/invoice depending on configured provider.
func (s *PaymentService) SendInvoice(ctx context.Context, bot *tgbotapi.BotAPI, user *models.User, chatID int64) error {
	plan, err := s.plans.GetDefault(ctx)
	if err != nil {
		return fmt.Errorf("get default plan: %w", err)
	}
	if plan == nil {
		return fmt.Errorf("no active plan configured")
	}

	switch strings.ToLower(s.cfg.PaymentProvider) {
	case "telegram", "":
		return s.sendTelegramInvoice(plan, bot, chatID)
	case "yookassa":
		return s.sendYooKassaPayment(ctx, plan, bot, user, chatID)
	default:
		return fmt.Errorf("unsupported payment provider: %s", s.cfg.PaymentProvider)
	}
}

func (s *PaymentService) sendTelegramInvoice(plan *models.Plan, bot *tgbotapi.BotAPI, chatID int64) error {
	prices := []tgbotapi.LabeledPrice{
		{
			Label:  fmt.Sprintf("%d кредитов", plan.Credits),
			Amount: plan.PriceMinorUnits,
		},
	}

	payload, _ := json.Marshal(map[string]any{
		"plan_id": plan.ID,
	})

	description := plan.Description
	if description == "" {
		description = "Пополнение баланса"
	}

	invoice := tgbotapi.NewInvoice(chatID,
		plan.Title,
		description,
		string(payload),
		s.cfg.TelegramPaymentProviderToken,
		"topup",
		plan.Currency,
		prices,
	)

	if _, err := bot.Send(invoice); err != nil {
		return fmt.Errorf("send invoice: %w", err)
	}
	return nil
}

func (s *PaymentService) sendYooKassaPayment(ctx context.Context, plan *models.Plan, bot *tgbotapi.BotAPI, user *models.User, chatID int64) error {
	payment, err := s.createYooKassaPayment(ctx, plan)
	if err != nil {
		return err
	}

	planID := plan.ID
	record := &models.Payment{
		UserID:         user.ID,
		PlanID:         &planID,
		Provider:       "yookassa",
		ProviderCharge: payment.ID,
		Currency:       plan.Currency,
		Amount:         plan.PriceMinorUnits,
		Status:         payment.Status,
		RawPayload:     string(jsonMustMarshal(payment)),
	}
	if err := s.payments.Create(ctx, record); err != nil {
		return fmt.Errorf("record payment: %w", err)
	}

	text := fmt.Sprintf("Оплата через ЮKassa:\nПлан: %s\nСумма: %.2f %s\nСсылка на оплату: %s\nПосле оплаты кредиты будут добавлены автоматически по webhook или вручную.",
		plan.Title, float64(plan.PriceMinorUnits)/100, plan.Currency, payment.Confirmation.URL)

	msg := tgbotapi.NewMessage(chatID, text)
	if _, err := bot.Send(msg); err != nil {
		return fmt.Errorf("send payment link: %w", err)
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
		PlanID int64 `json:"plan_id"`
	}
	if err := json.Unmarshal([]byte(payment.InvoicePayload), &payload); err != nil {
		return fmt.Errorf("parse payment payload: %w", err)
	}

	plan, err := s.planFromPayload(ctx, payload.PlanID)
	if err != nil {
		return err
	}
	if plan == nil {
		return fmt.Errorf("no plan available for payment recording")
	}

	if err := s.users.UpdatePaidCredits(ctx, user.ID, plan.Credits); err != nil {
		return fmt.Errorf("add paid credits: %w", err)
	}

	planID := plan.ID
	record := &models.Payment{
		UserID:         user.ID,
		PlanID:         &planID,
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

	return nil
}

func (s *PaymentService) planFromPayload(ctx context.Context, planID int64) (*models.Plan, error) {
	var plan *models.Plan
	var err error
	if planID > 0 {
		plan, err = s.plans.GetByID(ctx, planID)
		if err != nil {
			return nil, fmt.Errorf("get plan: %w", err)
		}
	}
	if plan == nil {
		plan, err = s.plans.GetDefault(ctx)
		if err != nil {
			return nil, fmt.Errorf("fallback plan: %w", err)
		}
	}
	return plan, nil
}

type yooPaymentResponse struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	Confirmation struct {
		Type string `json:"type"`
		URL  string `json:"confirmation_url"`
	} `json:"confirmation"`
	Amount struct {
		Value    string `json:"value"`
		Currency string `json:"currency"`
	} `json:"amount"`
}

func (s *PaymentService) createYooKassaPayment(ctx context.Context, plan *models.Plan) (*yooPaymentResponse, error) {
	if s.cfg.YooKassaShopID == "" || s.cfg.YooKassaSecretKey == "" {
		return nil, fmt.Errorf("yookassa credentials are not configured")
	}

	value := fmt.Sprintf("%.2f", float64(plan.PriceMinorUnits)/100)
	returnURL := s.cfg.YooKassaReturnURL
	if returnURL == "" {
		returnURL = "https://t.me"
	}

	payload := map[string]any{
		"amount": map[string]string{
			"value":    value,
			"currency": plan.Currency,
		},
		"confirmation": map[string]string{
			"type":       "redirect",
			"return_url": returnURL,
		},
		"description": fmt.Sprintf("%s (%d credits)", plan.Title, plan.Credits),
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.yookassa.ru/v3/payments", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("build yookassa request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotence-Key", fmt.Sprintf("%d-%d", plan.ID, time.Now().UnixNano()))
	req.SetBasicAuth(s.cfg.YooKassaShopID, s.cfg.YooKassaSecretKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("yookassa request: %w", err)
	}
	defer resp.Body.Close()

	var parsed yooPaymentResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode yookassa response: %w", err)
	}
	if parsed.ID == "" || parsed.Confirmation.URL == "" {
		return nil, fmt.Errorf("invalid yookassa response (missing id or confirmation url)")
	}
	if parsed.Status == "" {
		parsed.Status = "pending"
	}
	return &parsed, nil
}

// HandleYooKassaWebhook processes payment status updates and credits the user.
func (s *PaymentService) HandleYooKassaWebhook(ctx context.Context, payload []byte) error {
	var evt struct {
		Event  string `json:"event"`
		Object struct {
			ID          string `json:"id"`
			Status      string `json:"status"`
			Description string `json:"description"`
			Amount      struct {
				Value    string `json:"value"`
				Currency string `json:"currency"`
			} `json:"amount"`
		} `json:"object"`
	}
	if err := json.Unmarshal(payload, &evt); err != nil {
		return fmt.Errorf("parse webhook: %w", err)
	}
	if evt.Object.ID == "" {
		return fmt.Errorf("webhook missing payment id")
	}

	pmt, err := s.payments.FindByProviderCharge(ctx, "yookassa", evt.Object.ID)
	if err != nil {
		return fmt.Errorf("find payment: %w", err)
	}
	if pmt == nil {
		return fmt.Errorf("payment not found for id=%s", evt.Object.ID)
	}
	if pmt.Status == "paid" {
		return nil // already processed
	}

	// Mark as paid only on success
	if evt.Object.Status == "succeeded" {
		if pmt.PlanID == nil {
			return fmt.Errorf("payment missing plan_id")
		}
		plan, err := s.plans.GetByID(ctx, *pmt.PlanID)
		if err != nil {
			return fmt.Errorf("get plan: %w", err)
		}
		if plan == nil {
			return fmt.Errorf("plan not found for payment")
		}
		if err := s.users.UpdatePaidCredits(ctx, pmt.UserID, plan.Credits); err != nil {
			return fmt.Errorf("add paid credits: %w", err)
		}
		if err := s.payments.UpdateStatus(ctx, pmt.ID, "paid", string(payload)); err != nil {
			return fmt.Errorf("update payment status: %w", err)
		}
		return nil
	}

	// For failed/canceled just update status
	if err := s.payments.UpdateStatus(ctx, pmt.ID, evt.Object.Status, string(payload)); err != nil {
		return fmt.Errorf("update payment status: %w", err)
	}
	return nil
}

func jsonMustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}
