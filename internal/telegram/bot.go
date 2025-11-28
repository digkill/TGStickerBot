package telegram

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/digkill/TGStickerBot/internal/config"
	"github.com/digkill/TGStickerBot/internal/models"
	"github.com/digkill/TGStickerBot/internal/service"
)

const maxReferenceImages = 8

var errReferenceNotImage = errors.New("reference not image")

type ImageStorage interface {
	Upload(ctx context.Context, data []byte, contentType string) (string, error)
}

type Bot struct {
	cfg                         config.Config
	api                         *tgbotapi.BotAPI
	log                         *slog.Logger
	users                       *service.UserService
	generation                  *service.GenerationService
	promo                       *service.PromoService
	payments                    *service.PaymentService
	storage                     ImageStorage
	state                       *StateManager
	httpClient                  *http.Client
	subscriptionChannelUsername string
	subscriptionChannelID       int64
	subscriptionChannelLink     string
}

func NewBot(cfg config.Config, api *tgbotapi.BotAPI, log *slog.Logger, users *service.UserService, generation *service.GenerationService, promo *service.PromoService, payments *service.PaymentService, storage ImageStorage) *Bot {
	username := strings.TrimSpace(cfg.SubscriptionChannelUsername)
	var channelID int64
	if cfg.SubscriptionChannelID != 0 {
		channelID = cfg.SubscriptionChannelID
	} else if username != "" {
		if id, err := strconv.ParseInt(username, 10, 64); err == nil && id != 0 {
			channelID = id
			username = ""
		}
	}
	link := strings.TrimSpace(cfg.SubscriptionChannelURL)
	if link == "" && username != "" {
		link = fmt.Sprintf("https://t.me/%s", username)
	}

	return &Bot{
		cfg:                         cfg,
		api:                         api,
		log:                         log,
		users:                       users,
		generation:                  generation,
		promo:                       promo,
		payments:                    payments,
		storage:                     storage,
		state:                       NewStateManager(),
		httpClient:                  &http.Client{Timeout: 60 * time.Second},
		subscriptionChannelUsername: username,
		subscriptionChannelID:       channelID,
		subscriptionChannelLink:     link,
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

	if len(msg.Photo) > 0 || msg.Document != nil {
		if err := b.handleReferenceImage(ctx, msg); err != nil {
			if errors.Is(err, errReferenceNotImage) {
				b.sendText(msg.Chat.ID, "Это не изображение. Пришлите фото или картинку.")
			} else {
				b.log.Error("reference upload failed", "err", err)
				b.sendText(msg.Chat.ID, "Не удалось сохранить референс, попробуйте снова.")
			}
		}
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
		b.sendText(msg.Chat.ID, "Нажмите /generate, чтобы начать генерацию.")
	}
}

func (b *Bot) handleSuccessfulPayment(ctx context.Context, msg *tgbotapi.Message) {
	user, _, err := b.ensureUser(ctx, msg.From, msg.Chat.ID)
	if err != nil {
		b.log.Error("ensure user payment", "err", err)
		return
	}
	if err := b.payments.HandleSuccessfulPayment(ctx, user, msg.SuccessfulPayment); err != nil {
		b.log.Error("process successful payment", "err", err)
		return
	}
	b.sendText(msg.Chat.ID, "Оплата успешно получена! Кредиты зачислены.")
}

func (b *Bot) handleCommand(ctx context.Context, msg *tgbotapi.Message) {
	switch msg.Command() {
	case "start":
		user, _, err := b.ensureUser(ctx, msg.From, msg.Chat.ID)
		if err != nil {
			b.log.Error("ensure user", "err", err)
			return
		}
		b.tryGrantSubscriptionBonus(ctx, user, msg.From, msg.Chat.ID, true)
		text := fmt.Sprintf(
			"Привет, %s!\n\nГенерация стоит 5 кредитов за изображение. Добавь до %d референсов и отправь промпт.\n\nКоманды:\n/generate — начать генерацию\n/clearrefs — очистить референсы\n/promo <код> — активировать промокод\n/balance — проверить баланс\n/buy — купить кредиты\n/bonus — получить бонус за подписку",
			user.FirstName, maxReferenceImages,
		)
		b.sendText(msg.Chat.ID, text)
	case "generate":
		if _, _, err := b.ensureUser(ctx, msg.From, msg.Chat.ID); err != nil {
			b.log.Error("ensure user", "err", err)
			return
		}
		b.promptModelSelection(msg.Chat.ID)
	case "promo":
		b.handlePromo(ctx, msg)
	case "balance":
		b.handleBalance(ctx, msg)
	case "buy":
		user, _, err := b.ensureUser(ctx, msg.From, msg.Chat.ID)
		if err != nil {
			b.log.Error("ensure user buy", "err", err)
			return
		}
		if err := b.payments.SendInvoice(ctx, b.api, user, msg.Chat.ID); err != nil {
			b.log.Error("send invoice", "err", err)
			b.sendText(msg.Chat.ID, "Не удалось отправить счет. Попробуйте позже.")
		}
	case "clearrefs":
		b.state.ClearReferences(msg.Chat.ID)
		b.sendText(msg.Chat.ID, "Референсы очищены.")
	case "bonus":
		user, _, err := b.ensureUser(ctx, msg.From, msg.Chat.ID)
		if err != nil {
			b.log.Error("ensure user bonus", "err", err)
			return
		}
		b.tryGrantSubscriptionBonus(ctx, user, msg.From, msg.Chat.ID, true)
	default:
		b.sendText(msg.Chat.ID, "Неизвестная команда. Используйте /generate.")
	}
}

func (b *Bot) handlePromo(ctx context.Context, msg *tgbotapi.Message) {
	user, _, err := b.ensureUser(ctx, msg.From, msg.Chat.ID)
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
		b.sendText(msg.Chat.ID, "Формат: /promo КОД")
		return
	}
	if err := b.promo.Apply(ctx, user.ID, code, b.cfg.PromoBonusGenerations); err != nil {
		switch err {
		case service.ErrPromoInvalid:
			b.sendText(msg.Chat.ID, "Промокод недействителен.")
		case service.ErrPromoAlreadyRedeemed:
			b.sendText(msg.Chat.ID, "Этот промокод уже использован.")
		default:
			b.log.Error("apply promo", "err", err)
			b.sendText(msg.Chat.ID, "Не удалось применить промокод, попробуйте позже.")
		}
		return
	}
	b.sendText(msg.Chat.ID, fmt.Sprintf("Промокод активирован! +%d кредитов.", b.cfg.PromoBonusGenerations))
}

func (b *Bot) handleBalance(ctx context.Context, msg *tgbotapi.Message) {
	user, _, err := b.ensureUser(ctx, msg.From, msg.Chat.ID)
	if err != nil {
		b.log.Error("ensure user balance", "err", err)
		return
	}
	text := fmt.Sprintf("Баланс:\nПромо кредиты: %d\nПлатные кредиты: %d", user.PromoCredits, user.PaidCredits)
	b.sendText(msg.Chat.ID, text)
}

func (b *Bot) promptModelSelection(chatID int64) {
	session := &Session{
		State:         StateAwaitingModel,
		AspectRatio:   "1:1",
		Resolution:    "1K",
		ReferenceURLs: make([]string, 0),
	}
	b.state.Set(chatID, session)
	btnFlux := tgbotapi.NewInlineKeyboardButtonData("Flux 2", string(models.ModelFlux2))
	btnNano := tgbotapi.NewInlineKeyboardButtonData("Nano Banana Pro", string(models.ModelNanoBanana))
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(btnFlux),
		tgbotapi.NewInlineKeyboardRow(btnNano),
	)
	msg := tgbotapi.NewMessage(chatID, "Выберите модель. Можно добавить до 8 референсов, затем отправьте промпт.")
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
		b.sendText(cb.Message.Chat.ID, "Пришлите до 8 изображений (если нужны референсы), затем отправьте промпт.")
	default:
		if _, err := b.api.Request(tgbotapi.NewCallback(cb.ID, "Неизвестный выбор")); err != nil {
			b.log.Error("callback error", "err", err)
		}
	}
}

func (b *Bot) handlePrompt(ctx context.Context, msg *tgbotapi.Message, session *Session) {
	if session.SelectedModel == "" {
		b.sendText(msg.Chat.ID, "Сначала выберите модель через /generate.")
		return
	}
	if strings.TrimSpace(msg.Text) == "" {
		b.sendText(msg.Chat.ID, "Промпт не может быть пустым.")
		return
	}
	user, _, err := b.ensureUser(ctx, msg.From, msg.Chat.ID)
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
	if len(session.ReferenceURLs) > 0 {
		req.InputURLs = append([]string(nil), session.ReferenceURLs...)
	}

	b.sendText(msg.Chat.ID, "Генерация началась, это может занять до пары минут. Я пришлю результат, как только он будет готов.")

	result, err := b.generation.Generate(ctx, user, req)
	if err != nil {
		if errors.Is(err, service.ErrCreditsRequired) {
			b.sendText(msg.Chat.ID, "Недостаточно кредитов. Используйте /buy для покупки или /promo для ввода промокода.")
			return
		}
		b.log.Error("generate", "err", err)
		b.sendText(msg.Chat.ID, "Не удалось запустить генерацию, попробуйте позже.")
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
			b.sendText(chatID, "Не удалось получить результат.")
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

func (b *Bot) handleReferenceImage(ctx context.Context, msg *tgbotapi.Message) error {
	var fileID string
	contentType := "image/jpeg"

	switch {
	case len(msg.Photo) > 0:
		photo := msg.Photo[len(msg.Photo)-1]
		fileID = photo.FileID
	case msg.Document != nil:
		if mt := strings.ToLower(msg.Document.MimeType); mt != "" && !strings.HasPrefix(mt, "image/") {
			return errReferenceNotImage
		}
		fileID = msg.Document.FileID
		if msg.Document.MimeType != "" {
			contentType = msg.Document.MimeType
		}
	default:
		return nil
	}

	data, detectedType, err := b.downloadFile(ctx, fileID)
	if err != nil {
		return err
	}
	if detectedType != "" {
		contentType = detectedType
	}

	url, err := b.storage.Upload(ctx, data, contentType)
	if err != nil {
		return err
	}

	session := b.state.Get(msg.Chat.ID)
	session.ReferenceURLs = append(session.ReferenceURLs, url)
	if len(session.ReferenceURLs) > maxReferenceImages {
		session.ReferenceURLs = session.ReferenceURLs[len(session.ReferenceURLs)-maxReferenceImages:]
	}
	b.state.Set(msg.Chat.ID, session)

	b.sendText(msg.Chat.ID, fmt.Sprintf("Референс сохранён (%d/%d). Можно отправить промпт.", len(session.ReferenceURLs), maxReferenceImages))
	return nil
}

func (b *Bot) downloadFile(ctx context.Context, fileID string) ([]byte, string, error) {
	file, err := b.api.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		return nil, "", fmt.Errorf("get file: %w", err)
	}
	if file.FilePath == "" {
		return nil, "", fmt.Errorf("file path empty")
	}
	url := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", b.api.Token, file.FilePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("build request: %w", err)
	}
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("download file: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("telegram file status: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read file body: %w", err)
	}
	ct, err := normalizeImageContentType(resp.Header.Get("Content-Type"), body)
	if err != nil {
		return nil, "", err
	}
	return body, ct, nil
}

func (b *Bot) ensureUser(ctx context.Context, from *tgbotapi.User, chatID int64) (*models.User, bool, error) {
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
	user, created, err := b.users.Ensure(ctx, telegramID, username, firstName, lastName, b.cfg.FreeDailyGenerations)
	if err != nil {
		return nil, false, err
	}
	return user, created, nil
}

func (b *Bot) sendText(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	if _, err := b.api.Send(msg); err != nil {
		b.log.Error("send text", "err", err)
	}
}

func (b *Bot) tryGrantSubscriptionBonus(ctx context.Context, user *models.User, from *tgbotapi.User, chatID int64, remindOnFail bool) {
	if b.cfg.SubscriptionBonusGenerations <= 0 {
		return
	}
	if user.SubscriptionBonusGranted {
		if remindOnFail {
			b.sendText(chatID, "Бонус за подписку уже получен и повторно не выдается.")
		}
		return
	}
	if b.subscriptionChannelUsername == "" && b.subscriptionChannelID == 0 {
		return
	}
	if from == nil {
		b.sendText(chatID, "Не удалось проверить подписку. Попробуйте снова.")
		return
	}

	subscribed, err := b.isUserSubscribed(ctx, int64(from.ID))
	if err != nil {
		b.log.Error("check subscription", "err", err)
		b.notifySubscriptionCheckError(chatID, err)
		return
	}

	if !subscribed {
		if remindOnFail {
			b.sendSubscriptionReminder(chatID)
		}
		return
	}

	if err := b.users.UpdatePromoCredits(ctx, user.ID, b.cfg.SubscriptionBonusGenerations); err != nil {
		b.log.Error("add subscription bonus credits", "err", err)
		b.sendText(chatID, "Не удалось выдать бонусные кредиты, попробуйте позже.")
		return
	}
	if err := b.users.SetSubscriptionBonusGranted(ctx, user.ID, true); err != nil {
		b.log.Error("mark subscription bonus granted", "err", err)
		b.sendText(chatID, "Кредиты начислены, но не удалось сохранить статус. Повторно получить бонус не выйдет.")
		return
	}

	user.SubscriptionBonusGranted = true
	user.PromoCredits += b.cfg.SubscriptionBonusGenerations

	b.sendText(chatID, fmt.Sprintf("Спасибо за подписку! +%d бонусных кредитов.", b.cfg.SubscriptionBonusGenerations))
}

func (b *Bot) isUserSubscribed(ctx context.Context, userID int64) (bool, error) {
	cfg := tgbotapi.ChatConfigWithUser{
		UserID: userID,
	}
	switch {
	case b.subscriptionChannelID != 0:
		cfg.ChatID = b.subscriptionChannelID
	case b.subscriptionChannelUsername != "":
		cfg.SuperGroupUsername = strings.TrimPrefix(b.subscriptionChannelUsername, "@")
	default:
		return false, fmt.Errorf("subscription channel not configured")
	}

	member, err := b.api.GetChatMember(tgbotapi.GetChatMemberConfig{ChatConfigWithUser: cfg})
	if err != nil {
		return false, err
	}

	status := strings.ToLower(member.Status)
	switch status {
	case "creator", "administrator", "member":
		return true, nil
	default:
		return false, nil
	}
}

func (b *Bot) sendSubscriptionReminder(chatID int64) {
	if b.subscriptionChannelLink != "" {
		b.sendText(chatID, fmt.Sprintf("Подпишитесь на канал %s и отправьте /bonus, чтобы получить %d бонусных кредитов (1 раз).", b.subscriptionChannelLink, b.cfg.SubscriptionBonusGenerations))
	} else {
		b.sendText(chatID, fmt.Sprintf("Подпишитесь на канал и отправьте /bonus, чтобы получить %d бонусных кредитов (1 раз).", b.cfg.SubscriptionBonusGenerations))
	}
}

func (b *Bot) notifySubscriptionCheckError(chatID int64, err error) {
	if err == nil {
		return
	}
	msg := err.Error()
	if strings.Contains(strings.ToLower(msg), "chat not found") {
		prompt := "Не удалось проверить подписку: канал недоступен."
		if b.subscriptionChannelLink != "" {
			prompt = fmt.Sprintf("%s Подпишитесь на %s и отправьте /bonus, чтобы получить кредиты (единожды).", prompt, b.subscriptionChannelLink)
		}
		b.sendText(chatID, prompt)
	} else {
		b.sendText(chatID, fmt.Sprintf("Не удалось проверить подписку: %s. Если вы подписались, отправьте /bonus, чтобы получить кредиты (один раз).", err.Error()))
	}
}

func normalizeImageContentType(headerCT string, data []byte) (string, error) {
	ct := strings.ToLower(strings.TrimSpace(headerCT))
	if idx := strings.Index(ct, ";"); idx > 0 {
		ct = ct[:idx]
	}
	if ct == "" || ct == "application/octet-stream" || !strings.HasPrefix(ct, "image/") {
		if len(data) > 0 {
			ct = http.DetectContentType(data)
			if idx := strings.Index(ct, ";"); idx > 0 {
				ct = ct[:idx]
			}
		}
	}

	switch ct {
	case "image/jpeg", "image/jpg":
		return "image/jpeg", nil
	case "image/png":
		return "image/png", nil
	case "image/webp":
		return "image/webp", nil
	default:
		return "", errReferenceNotImage
	}
}
