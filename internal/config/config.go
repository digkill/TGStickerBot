package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
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
	SubscriptionChannelURL       string
	SubscriptionChannelUsername  string
	SubscriptionChannelID        int64
	SubscriptionBonusGenerations int
	TelegramPaymentProviderToken string
	PaymentCurrency              string
	PaymentPriceMinorUnits       int
	PaymentCreditsPerPackage     int
	PaymentProvider              string
	YooKassaShopID               string
	YooKassaSecretKey            string
	YooKassaReturnURL            string
	AdminListenAddr              string
	AdminUsername                string
	AdminPassword                string
	S3Endpoint                   string
	S3Region                     string
	S3AccessKey                  string
	S3SecretKey                  string
	S3Bucket                     string
	S3PublicBaseURL              string
	S3UsePathStyle               bool
	S3Prefix                     string
}

// Load reads configuration from environment variables, applying sane defaults.
func Load() (Config, error) {
	if err := loadEnvFile(); err != nil {
		return Config{}, err
	}

	const defaultKIEBaseURL = "https://api.kie.ai"

	cfg := Config{
		KIEBaseURL:                   normalizeKIEBaseURL(getEnv("KIE_BASE_URL", defaultKIEBaseURL), defaultKIEBaseURL),
		Flux2Path:                    getEnv("KIE_FLUX2_PATH", "/api/v1/run/flux-2"),
		NanoBananaPath:               getEnv("KIE_NANO_BANANA_PATH", "/api/v1/run/nano-banana-pro"),
		RequestTimeout:               time.Second * time.Duration(getInt("HTTP_TIMEOUT_SECONDS", 60)),
		FreeDailyGenerations:         getInt("FREE_DAILY_GENERATIONS", 0),
		PromoBonusGenerations:        getInt("PROMO_BONUS_GENERATIONS", 100),
		SubscriptionChannelURL:       getEnv("SUBSCRIPTION_CHANNEL_URL", ""),
		SubscriptionChannelUsername:  normalizeChannelUsername(getEnv("SUBSCRIPTION_CHANNEL_USERNAME", "")),
		SubscriptionChannelID:        getInt64("SUBSCRIPTION_CHANNEL_ID", 0),
		SubscriptionBonusGenerations: getInt("SUBSCRIPTION_BONUS_GENERATIONS", 100),
		PaymentCurrency:              getEnv("PAYMENT_CURRENCY", "RUB"),
		PaymentPriceMinorUnits:       getInt("PAYMENT_PRICE_MINOR_UNITS", 29900),
		PaymentCreditsPerPackage:     getInt("PAYMENT_CREDITS_PER_PACKAGE", 50),
		PaymentProvider:              strings.ToLower(getEnv("PAYMENT_PROVIDER", "telegram")),
		YooKassaShopID:               getEnv("YOOKASSA_SHOP_ID", ""),
		YooKassaSecretKey:            getEnv("YOOKASSA_SECRET_KEY", ""),
		YooKassaReturnURL:            getEnv("YOOKASSA_RETURN_URL", ""),
		AdminListenAddr:              getEnv("ADMIN_LISTEN_ADDR", ":8080"),
		AdminUsername:                getEnv("ADMIN_USERNAME", "admin"),
		AdminPassword:                getEnv("ADMIN_PASSWORD", "change-me"),
		S3Endpoint:                   getEnv("S3_ENDPOINT", ""),
		S3Region:                     os.Getenv("S3_REGION"),
		S3AccessKey:                  os.Getenv("S3_ACCESS_KEY"),
		S3SecretKey:                  os.Getenv("S3_SECRET_KEY"),
		S3Bucket:                     os.Getenv("S3_BUCKET"),
		S3PublicBaseURL:              os.Getenv("S3_PUBLIC_BASE_URL"),
		S3UsePathStyle:               getBool("S3_USE_PATH_STYLE", false),
		S3Prefix:                     getEnv("S3_PREFIX", "references"),
	}

	cfg.BotToken = os.Getenv("TELEGRAM_BOT_TOKEN")
	cfg.MySQLDSN = os.Getenv("MYSQL_DSN")
	cfg.KIEAPIKey = os.Getenv("KIE_API_KEY")
	cfg.TelegramPaymentProviderToken = os.Getenv("TELEGRAM_PAYMENT_PROVIDER_TOKEN")

	if cfg.SubscriptionChannelUsername == "" && cfg.SubscriptionChannelURL != "" {
		if username := extractChannelUsername(cfg.SubscriptionChannelURL); username != "" {
			cfg.SubscriptionChannelUsername = username
		}
	}

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
		if cfg.PaymentProvider == "telegram" {
			missing = append(missing, "TELEGRAM_PAYMENT_PROVIDER_TOKEN")
		}
	}
	if cfg.PaymentProvider == "yookassa" {
		if cfg.YooKassaShopID == "" {
			missing = append(missing, "YOOKASSA_SHOP_ID")
		}
		if cfg.YooKassaSecretKey == "" {
			missing = append(missing, "YOOKASSA_SECRET_KEY")
		}
	}
	if cfg.S3Region == "" {
		missing = append(missing, "S3_REGION")
	}
	if cfg.S3AccessKey == "" {
		missing = append(missing, "S3_ACCESS_KEY")
	}
	if cfg.S3SecretKey == "" {
		missing = append(missing, "S3_SECRET_KEY")
	}
	if cfg.S3Bucket == "" {
		missing = append(missing, "S3_BUCKET")
	}
	if cfg.S3PublicBaseURL == "" {
		missing = append(missing, "S3_PUBLIC_BASE_URL")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required environment variables: %v", missing)
	}

	return cfg, nil
}

// normalizeKIEBaseURL ensures we always hit the documented API host. Some docs and UI pages
// use the root kie.ai domain, which returns HTML instead of JSON and causes 404s.
func normalizeKIEBaseURL(raw string, fallback string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return fallback
	}

	if parsed.Scheme == "" {
		parsed.Scheme = "https"
	}
	if parsed.Host == "" {
		parsed.Host = parsed.Path
		parsed.Path = ""
	}

	// Force API subdomain to avoid landing on the marketing site.
	if parsed.Host == "kie.ai" {
		parsed.Host = "api.kie.ai"
	}

	return parsed.String()
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

func getInt64(key string, fallback int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	i, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fallback
	}
	return i
}

func getBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func loadEnvFile() error {
	candidates := []string{}
	if custom, ok := os.LookupEnv("CONFIG_ENV_PATH"); ok && custom != "" {
		candidates = append(candidates, custom)
	}
	candidates = append(candidates,
		`D:\StickerBot\configs\.env`,
		filepath.Join("configs", ".env"),
		".env",
	)

	for _, path := range candidates {
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("access env file %s: %w", path, err)
		}
		if info.IsDir() {
			continue
		}
		if err := godotenv.Overload(path); err != nil {
			return fmt.Errorf("load env file %s: %w", path, err)
		}
		return nil
	}
	return fmt.Errorf("env file not found; tried %v", candidates)
}

func normalizeChannelUsername(username string) string {
	username = strings.TrimSpace(username)
	username = strings.TrimPrefix(username, "@")
	return username
}

func extractChannelUsername(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, "/")
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		if parsed, err := url.Parse(raw); err == nil {
			path := strings.Trim(parsed.Path, "/")
			if path != "" {
				return normalizeChannelUsername(path)
			}
		}
	}
	if strings.HasPrefix(raw, "t.me/") {
		raw = strings.TrimPrefix(raw, "t.me/")
	}
	return normalizeChannelUsername(raw)
}
