package main

import (
	"context"
	"errors"
	"log"
	"os/signal"
	"syscall"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/example/stickerbot/internal/admin"
	"github.com/example/stickerbot/internal/config"
	"github.com/example/stickerbot/internal/database"
	"github.com/example/stickerbot/internal/kie"
	"github.com/example/stickerbot/internal/repository"
	"github.com/example/stickerbot/internal/service"
	"github.com/example/stickerbot/internal/telegram"
	"github.com/example/stickerbot/pkg/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	logr := logger.New()

	db, err := database.Connect(cfg)
	if err != nil {
		log.Fatalf("database connect: %v", err)
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := database.Migrate(ctx, db); err != nil {
		log.Fatalf("database migrate: %v", err)
	}

	botAPI, err := tgbotapi.NewBotAPI(cfg.BotToken)
	if err != nil {
		log.Fatalf("telegram bot: %v", err)
	}

	kieClient := kie.NewClient(cfg)

	userRepo := repository.NewUserRepository(db)
	generationRepo := repository.NewGenerationRepository(db)
	promoRepo := repository.NewPromoRepository(db)
	paymentRepo := repository.NewPaymentRepository(db)

	userService := service.NewUserService(userRepo)
	generationService := service.NewGenerationService(cfg, logr, userRepo, generationRepo, kieClient)
	promoService := service.NewPromoService(promoRepo, userRepo)
	paymentService := service.NewPaymentService(cfg, logr, paymentRepo, userRepo)

	bot := telegram.NewBot(cfg, botAPI, logr, userService, generationService, promoService, paymentService)

	adminServer := admin.NewServer(cfg.AdminListenAddr, cfg.AdminUsername, cfg.AdminPassword, logr, userService, botAPI)
	go func() {
		if err := adminServer.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logr.Error("admin server stopped", "err", err)
		}
	}()

	if err := bot.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logr.Error("bot stopped", "err", err)
	}
}
