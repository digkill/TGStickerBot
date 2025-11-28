package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/example/stickerbot/internal/config"
	"github.com/example/stickerbot/internal/models"
	"github.com/example/stickerbot/internal/service"
)

type Bot struct {
	cfg        config.Config
	api        *tgbotapi.BotAPI
	log        *slog.Logger
	users      *service.UserService
	generation *service.GenerationService
	promo      *service.PromoService
	payments   *service.PaymentService
	state      *StateManager
}

func NewBot(cfg config.Config, api *tgbotapi.BotAPI, log *slog.Logger, users *service.UserService, generation *service.GenerationService, promo *service.PromoService, payments *service.PaymentService) *Bot {
	return &Bot{
		cfg:        cfg,
		api:        api,
		log:        log,
		users:      users,
		generation: generation,
		promo:      promo,
		payments:   payments,
		state:      NewStateManager(),
	}
}

func (b *Bot) Run(ctx context.Context) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)
	b.log.Info("telegram bot started")

	for {
		select {
		case update := <-updates:
			if update.Message != nil {
				b.handleMessage(ctx, update.Message)
			} else if update.CallbackQuery != nil {
				b.handleCallback(ctx, update.CallbackQuery)
			} else if update.PreCheckoutQuery != nil {
				if err := b.payments.HandlePreCheckout(b.api, update.PreCheckoutQuery); err != nil {
					b.log.Error("pre-checkout failed", "err", err)
				}
			}
		case <-ctx.Done():
			b.api.StopReceivingUpdates()
			return ctx.Err()
		}
	}
}

func (b *Bot) handleMessage(ctx context.Context, msg *tgbotapi.Message) {
	if msg.SuccessfulPayment != nil {
		b.handleSuccessfulPayment(ctx, msg)
		return
	}

	if msg.IsCommand() {
		b.handleCommand(ctx, msg)
		return
	}

	session := b.state.Get(msg.Chat.ID)
	switch session.State {
	case StateAwaitingPrompt:
		b.handlePrompt(ctx, msg, session)
	default:
		b.sendText(msg.Chat.ID, "Используйте /generate для создания стикера или изображения.")
	}
}

func (b *Bot) handleSuccessfulPayment(ctx context.Context, msg *tgbotapi.Message) {
	user, err := b.ensureUser(ctx, msg.From, msg.Chat.ID)
	if err != nil {
		b.log.Error("ensure user payment", "err", err)
		return
	}
	if err := b.payments.HandleSuccessfulPayment(ctx, user, msg.SuccessfulPayment); err != nil {
		b.log.Error("process successful payment", "err", err)
		return
	}
	b.sendText(msg.Chat.ID, "Оплата прошла успешно! Кредиты начислены на ваш счет.")
}

func (b *Bot) handleCommand(ctx context.Context, msg *tgbotapi.Message) {
	switch msg.Command() {
	case "start":
		user, err := b.ensureUser(ctx, msg.From, msg.Chat.ID)
		if err != nil {
			b.log.Error("ensure user", "err", err)
			return
		}
		text := fmt.Sprintf("Привет, %s!\n\nЯ помогу тебе сгенерировать стикеры и изображения. Бесплатный лимит: %d в день.\nКоманды:\n/generate — создать стикер\n/promo <код> — активировать промокод\n/balance — проверить оставшиеся генерации\n/buy — пополнить баланс", user.FirstName, user.FreeDailyLimit)
		b.sendText(msg.Chat.ID, text)
	case "generate":
		if _, err := b.ensureUser(ctx, msg.From, msg.Chat.ID); err != nil {
			b.log.Error("ensure user", "err", err)
			return
		}
		b.promptModelSelection(msg.Chat.ID)
	case "promo":
		b.handlePromo(ctx, msg)
	case "balance":
		b.handleBalance(ctx, msg)
	case "buy":
		if err := b.payments.SendInvoice(b.api, msg.Chat.ID); err != nil {
			b.log.Error("send invoice", "err", err)
			b.sendText(msg.Chat.ID, "Не удалось отправить инвойс, попробуйте позже.")
		}
	default:
		b.sendText(msg.Chat.ID, "Неизвестная команда. Попробуйте /generate.")
	}
}

func (b *Bot) handlePromo(ctx context.Context, msg *tgbotapi.Message) {
	user, err := b.ensureUser(ctx, msg.From, msg.Chat.ID)
	if err != nil {
		b.log.Error("ensure user promo", "err", err)
		return
	}
	args := strings.TrimSpace(strings.TrimPrefix(msg.Text, "/promo"))
	if args == "" && len(msg.CommandArguments()) > 0 {
		args = msg.CommandArguments()
	}
	code := strings.TrimSpace(args)
	if code == "" {
		b.sendText(msg.Chat.ID, "Введите промокод: /promo ВАШ_КОД")
		return
	}
	if err := b.promo.Apply(ctx, user.ID, code, b.cfg.PromoBonusGenerations); err != nil {
		switch err {
		case service.ErrPromoInvalid:
			b.sendText(msg.Chat.ID, "Промокод не существует.")
		case service.ErrPromoAlreadyRedeemed:
			b.sendText(msg.Chat.ID, "Вы уже активировали этот промокод.")
		default:
			b.log.Error("apply promo", "err", err)
			b.sendText(msg.Chat.ID, "Не удалось применить промокод, попробуйте позже.")
		}
		return
	}
	b.sendText(msg.Chat.ID, fmt.Sprintf("Промокод активирован! +%d генераций.", b.cfg.PromoBonusGenerations))
}

func (b *Bot) handleBalance(ctx context.Context, msg *tgbotapi.Message) {
	user, err := b.ensureUser(ctx, msg.From, msg.Chat.ID)
	if err != nil {
		b.log.Error("ensure user balance", "err", err)
		return
	}
	count, err := b.generation.DailyCount(ctx, user.ID)
	if err != nil {
		b.log.Error("count daily", "err", err)
		return
	}
	freeLeft := user.FreeDailyLimit - count
	if freeLeft < 0 {
		freeLeft = 0
	}
	text := fmt.Sprintf("Баланс:\nСвободно сегодня: %d\nПромо кредиты: %d\nПлатные кредиты: %d", freeLeft, user.PromoCredits, user.PaidCredits)
	b.sendText(msg.Chat.ID, text)
}

func (b *Bot) promptModelSelection(chatID int64) {
	session := &Session{
		State:       StateAwaitingModel,
		AspectRatio: "1:1",
		Resolution:  "1K",
	}
	b.state.Set(chatID, session)
	btnFlux := tgbotapi.NewInlineKeyboardButtonData("Flux 2", string(models.ModelFlux2))
	btnNano := tgbotapi.NewInlineKeyboardButtonData("Nano Banana Pro", string(models.ModelNanoBanana))
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(btnFlux),
		tgbotapi.NewInlineKeyboardRow(btnNano),
	)
	msg := tgbotapi.NewMessage(chatID, "Выберите модель генерации:")
	msg.ReplyMarkup = keyboard
	if _, err := b.api.Send(msg); err != nil {
		b.log.Error("send keyboard", "err", err)
	}
}

func (b *Bot) handleCallback(ctx context.Context, cb *tgbotapi.CallbackQuery) {
	switch cb.Data {
	case string(models.ModelFlux2), string(models.ModelNanoBanana):
		session := b.state.Get(cb.Message.Chat.ID)
		session.State = StateAwaitingPrompt
		session.SelectedModel = models.ModelType(cb.Data)
		b.state.Set(cb.Message.Chat.ID, session)
		if _, err := b.api.Request(tgbotapi.NewCallback(cb.ID, "Модель выбрана")); err != nil {
			b.log.Error("callback ack", "err", err)
		}
		b.sendText(cb.Message.Chat.ID, "Отправьте текстовый промпт для генерации.")
	default:
		if _, err := b.api.Request(tgbotapi.NewCallback(cb.ID, "Неизвестное действие")); err != nil {
			b.log.Error("callback error", "err", err)
		}
	}
}

func (b *Bot) handlePrompt(ctx context.Context, msg *tgbotapi.Message, session *Session) {
	if session.SelectedModel == "" {
		b.sendText(msg.Chat.ID, "Сначала выберите модель — командой /generate.")
		return
	}
	if strings.TrimSpace(msg.Text) == "" {
		b.sendText(msg.Chat.ID, "Введите текстовый промпт для генерации.")
		return
	}
	user, err := b.ensureUser(ctx, msg.From, msg.Chat.ID)
	if err != nil {
		b.log.Error("ensure user prompt", "err", err)
		return
	}

	req := service.GenerationRequest{
		Model:       session.SelectedModel,
		Prompt:      msg.Text,
		AspectRatio: session.AspectRatio,
		Resolution:  session.Resolution,
	}

	result, err := b.generation.Generate(ctx, user, req)
	if err != nil {
		if errors.Is(err, service.ErrCreditsRequired) {
			b.sendText(msg.Chat.ID, "Лимит исчерпан. Используйте /buy для пополнения или введите промокод.")
			return
		}
		b.log.Error("generate", "err", err)
		b.sendText(msg.Chat.ID, "Не удалось сгенерировать изображение, попробуйте позже.")
		return
	}

	b.deliverImage(msg.Chat.ID, result)
	b.state.Reset(msg.Chat.ID)
}

func (b *Bot) deliverImage(chatID int64, result *service.GenerationResult) {
	var cfg tgbotapi.PhotoConfig
	switch {
	case result.Image.URL != "":
		cfg = tgbotapi.NewPhoto(chatID, tgbotapi.FileURL(result.Image.URL))
	default:
		if len(result.Image.Bytes) == 0 {
			b.sendText(chatID, "Сервис вернул пустое изображение.")
			return
		}
		cfg = tgbotapi.NewPhoto(chatID, tgbotapi.FileBytes{
			Name:  "generation.png",
			Bytes: result.Image.Bytes,
		})
	}
	cfg.Caption = fmt.Sprintf("Модель: %s\nТип списания: %s", result.Model, result.Cost)
	if _, err := b.api.Send(cfg); err != nil {
		b.log.Error("send image", "err", err)
	}
}

func (b *Bot) ensureUser(ctx context.Context, from *tgbotapi.User, chatID int64) (*models.User, error) {
	username := ""
	if from != nil {
		username = from.UserName
	}
	firstName := ""
	lastName := ""
	if from != nil {
		firstName = from.FirstName
		lastName = from.LastName
	}
	telegramID := chatID
	if from != nil {
		telegramID = int64(from.ID)
	}
	user, err := b.users.Ensure(ctx, telegramID, username, firstName, lastName, b.cfg.FreeDailyGenerations)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (b *Bot) sendText(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	if _, err := b.api.Send(msg); err != nil {
		b.log.Error("send text", "err", err)
	}
}
