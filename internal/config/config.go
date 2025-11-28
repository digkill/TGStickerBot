package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config aggregates runtime configuration for the bot and supporting services.
type Config struct {
	BotToken                     string
	MySQLDSN                     string
	KIEAPIKey                    string
	KIEBaseURL                   string
	Flux2Path                    string
	NanoBananaPath               string
	RequestTimeout               time.Duration
	FreeDailyGenerations         int
	PromoBonusGenerations        int
	TelegramPaymentProviderToken string
	PaymentCurrency              string
	PaymentPriceMinorUnits       int
	PaymentCreditsPerPackage     int
	AdminListenAddr              string
	AdminUsername                string
	AdminPassword                string
}

// Load reads configuration from environment variables, applying sane defaults.
func Load() (Config, error) {
	cfg := Config{
		KIEBaseURL:               getEnv("KIE_BASE_URL", "https://kie.ai"),
		Flux2Path:                getEnv("KIE_FLUX2_PATH", "/flux-2"),
		NanoBananaPath:           getEnv("KIE_NANO_BANANA_PATH", "/nano-banana-pro"),
		RequestTimeout:           time.Second * time.Duration(getInt("HTTP_TIMEOUT_SECONDS", 60)),
		FreeDailyGenerations:     getInt("FREE_DAILY_GENERATIONS", 5),
		PromoBonusGenerations:    getInt("PROMO_BONUS_GENERATIONS", 100),
		PaymentCurrency:          getEnv("PAYMENT_CURRENCY", "RUB"),
		PaymentPriceMinorUnits:   getInt("PAYMENT_PRICE_MINOR_UNITS", 29900),
		PaymentCreditsPerPackage: getInt("PAYMENT_CREDITS_PER_PACKAGE", 50),
		AdminListenAddr:          getEnv("ADMIN_LISTEN_ADDR", ":8080"),
		AdminUsername:            getEnv("ADMIN_USERNAME", "admin"),
		AdminPassword:            getEnv("ADMIN_PASSWORD", "change-me"),
	}

	cfg.BotToken = os.Getenv("TELEGRAM_BOT_TOKEN")
	cfg.MySQLDSN = os.Getenv("MYSQL_DSN")
	cfg.KIEAPIKey = os.Getenv("KIE_API_KEY")
	cfg.TelegramPaymentProviderToken = os.Getenv("TELEGRAM_PAYMENT_PROVIDER_TOKEN")

	var missing []string
	if cfg.BotToken == "" {
		missing = append(missing, "TELEGRAM_BOT_TOKEN")
	}
	if cfg.MySQLDSN == "" {
		missing = append(missing, "MYSQL_DSN")
	}
	if cfg.KIEAPIKey == "" {
		missing = append(missing, "KIE_API_KEY")
	}
	if cfg.TelegramPaymentProviderToken == "" {
		missing = append(missing, "TELEGRAM_PAYMENT_PROVIDER_TOKEN")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required environment variables: %v", missing)
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return i
}
