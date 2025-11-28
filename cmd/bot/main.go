package main

import (
	"context"
	"errors"
	"log"
	"os/signal"
	"syscall"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/digkill/TGStickerBot/internal/admin"
	"github.com/digkill/TGStickerBot/internal/config"
	"github.com/digkill/TGStickerBot/internal/database"
	"github.com/digkill/TGStickerBot/internal/kie"
	"github.com/digkill/TGStickerBot/internal/repository"
	"github.com/digkill/TGStickerBot/internal/service"
	"github.com/digkill/TGStickerBot/internal/storage"
	"github.com/digkill/TGStickerBot/internal/telegram"
	"github.com/digkill/TGStickerBot/pkg/logger"
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

	kieClient := kie.NewClient(cfg, logr)

	userRepo := repository.NewUserRepository(db)
	generationRepo := repository.NewGenerationRepository(db)
	promoRepo := repository.NewPromoRepository(db)
	paymentRepo := repository.NewPaymentRepository(db)
	planRepo := repository.NewPlanRepository(db)

	userService := service.NewUserService(userRepo)
	planService := service.NewPlanService(cfg, planRepo)
	generationService := service.NewGenerationService(cfg, logr, userRepo, generationRepo, kieClient)
	promoService := service.NewPromoService(promoRepo, userRepo)
	paymentService := service.NewPaymentService(cfg, paymentRepo, userRepo, planService)

	if err := planService.EnsureDefaultPlan(ctx); err != nil {
		log.Fatalf("ensure default plan: %v", err)
	}

	uploader, err := storage.NewUploader(storage.Config{
		Endpoint:      cfg.S3Endpoint,
		Region:        cfg.S3Region,
		AccessKey:     cfg.S3AccessKey,
		SecretKey:     cfg.S3SecretKey,
		Bucket:        cfg.S3Bucket,
		PublicBaseURL: cfg.S3PublicBaseURL,
		UsePathStyle:  cfg.S3UsePathStyle,
		Prefix:        cfg.S3Prefix,
	})
	if err != nil {
		log.Fatalf("storage uploader: %v", err)
	}

	bot := telegram.NewBot(cfg, botAPI, logr, userService, generationService, promoService, paymentService, uploader)

	adminServer := admin.NewServer(cfg.AdminListenAddr, cfg.AdminUsername, cfg.AdminPassword, logr, userService, planService, promoService, paymentService, botAPI)
	go func() {
		if err := adminServer.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logr.Error("admin server stopped", "err", err)
		}
	}()

	if err := bot.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logr.Error("bot stopped", "err", err)
	}
}
