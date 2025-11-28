# StickerBot (Go + Telegram)

Телеграм-бот для генерации стикеров и изображений через KIE API (Flux 2 и Nano Banana Pro) с поддержкой платных подписок, промокодов и административной панели для рассылок.

## Возможности

- Генерация изображений/стикеров через модели Flux 2 и Nano Banana Pro (`https://kie.ai/flux-2`, `https://kie.ai/nano-banana-pro`).
- Бесплатный дневной лимит (по умолчанию 5 генераций).
- Промокоды c бонусом (по умолчанию +100 генераций).
- Платное пополнение через платежи Telegram.
- Админ-панель (HTTP) для отправки пушей всем пользователям.
- Хранение пользователей, генераций, промо и платежей в MySQL.

## Стек

- Go 1.21+
- Telegram Bot API (`github.com/go-telegram-bot-api/telegram-bot-api/v5`)
- MySQL (`github.com/go-sql-driver/mysql`)
- HTTP-роутер `chi` для админки

## Настройка

1. Создайте и заполните файл `.env` по образцу `.env.example`.
2. Подготовьте MySQL базу (таблицы создаются автоматически при старте).
3. Установите зависимости и соберите бота:

```bash
go mod tidy
go build ./cmd/bot
```

4. Запустите бота:

```bash
go run ./cmd/bot
```

## Основные переменные окружения

| Переменная | Описание |
|-----------|----------|
| `TELEGRAM_BOT_TOKEN` | токен бота |
| `TELEGRAM_PAYMENT_PROVIDER_TOKEN` | провайдер токен для платежей |
| `MYSQL_DSN` | DSN подключения к MySQL (`user:pass@tcp(host:3306)/dbname?parseTime=true&loc=UTC`) |
| `KIE_API_KEY` | API ключ для KIE |
| `FREE_DAILY_GENERATIONS` | дневной бесплатный лимит (3-5) |
| `PROMO_BONUS_GENERATIONS` | бонус по промокоду (по умолчанию 100) |
| `ADMIN_LISTEN_ADDR` | адрес админ-панели (например, `:8080`) |
| `ADMIN_USERNAME` / `ADMIN_PASSWORD` | учетные данные для панели |

Полный список смотрите в `.env.example`.

## Административная панель

HTTP-панель стартует на `ADMIN_LISTEN_ADDR`. Доступна ручка `POST /broadcast` (Basic Auth).

Пример запроса:

```bash
curl -u admin:passwd -H "Content-Type: application/json" \
  -d '{"message":"Новая акция!"}' \
  http://localhost:8080/broadcast
```

## Заметки по KIE API

- Авторизация реализована через заголовок `Authorization: Bearer <KIE_API_KEY>`.
- Flux 2 ожидает поля: `prompt`, `aspect_ratio`, `resolution`, `input_urls` (опционально).
- Nano Banana Pro поддерживает `prompt`, `aspect_ratio`, `resolution`, `image_input` (опционально) и `output_format` (`png`/`jpg`).
- Ответ сервиса должен содержать `image_url` или `image_base64`. В случае `base64` бот отправляет файл напрямую.

## Ограничения и TODO

- Поддержка reference-изображений требует размещения файлов по публичным URL (нужен CDN/S3).
- Для реального продакшена рекомендуется добавить ретраи и метрики.
- Управление тарифами и промокодами лучше вынести в отдельный CRUD интерфейс.

## Лицензия

MIT.

