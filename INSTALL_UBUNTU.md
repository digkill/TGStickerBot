# Установка бота на Ubuntu (systemd)

Инструкция для продакшена с единым бинарём и сервисом systemd.

## Подготовка окружения
1. Обновить пакеты:
   ```bash
   sudo apt update && sudo apt upgrade -y
   sudo apt install -y git curl unzip
   ```
2. Установить Go (если нет) — пример для 1.21+:
   ```bash
   cd /tmp
   curl -LO https://go.dev/dl/go1.21.10.linux-amd64.tar.gz
   sudo rm -rf /usr/local/go
   sudo tar -C /usr/local -xzf go1.21.10.linux-amd64.tar.gz
   echo 'export PATH=$PATH:/usr/local/go/bin' | sudo tee /etc/profile.d/go.sh
   source /etc/profile.d/go.sh
   ```
3. Создать пользователя без shell:
   ```bash
   sudo useradd -r -m -d /opt/stickerbot -s /usr/sbin/nologin stickerbot
   ```

## Деплой кода и сборка
```bash
sudo -u stickerbot -s <<'EOF'
cd /opt/stickerbot
git clone https://example.com/your/repo.git .   # или скопируйте ваш код
go mod download
go build -o stickerbot-bot ./cmd/bot
EOF
```

## Настройка переменных окружения
Создайте файл `/etc/default/stickerbot`:
```bash
sudo tee /etc/default/stickerbot >/dev/null <<'ENV'
TELEGRAM_BOT_TOKEN=xxx
TELEGRAM_PAYMENT_PROVIDER_TOKEN=xxx
MYSQL_DSN=user:pass@tcp(localhost:3306)/stickerbot?parseTime=true&loc=UTC
KIE_API_KEY=xxx

# KIE
KIE_BASE_URL=https://api.kie.ai
KIE_FLUX2_PATH=/api/v1/run/flux-2
KIE_NANO_BANANA_PATH=/api/v1/run/nano-banana-pro

# Бонусы/лимиты/оплаты
FREE_DAILY_GENERATIONS=0
PROMO_BONUS_GENERATIONS=100
HTTP_TIMEOUT_SECONDS=60

# Подписка
SUBSCRIPTION_CHANNEL_USERNAME=noname_mem      # без @
SUBSCRIPTION_CHANNEL_ID=                      # либо ID формата -100...
SUBSCRIPTION_BONUS_GENERATIONS=100

# S3
S3_ENDPOINT=https://s3.example.com
S3_REGION=us-east-1
S3_ACCESS_KEY=xxx
S3_SECRET_KEY=xxx
S3_BUCKET=stickerbot
S3_PUBLIC_BASE_URL=https://cdn.example.com
S3_USE_PATH_STYLE=true
S3_PREFIX=references
ENV
```
Дайте права:
```bash
sudo chown root:stickerbot /etc/default/stickerbot
sudo chmod 640 /etc/default/stickerbot
```

## Конфигурация systemd
Скопируйте unit-файл:
```bash
sudo cp /opt/stickerbot/deploy/systemd/stickerbot.service /etc/systemd/system/stickerbot.service
sudo systemctl daemon-reload
sudo systemctl enable stickerbot.service
```

## Запуск
```bash
sudo systemctl start stickerbot.service
sudo systemctl status stickerbot.service
```

Логи:
```bash
journalctl -u stickerbot.service -f
```

## Обновление
```bash
sudo -u stickerbot -s <<'EOF'
cd /opt/stickerbot
git pull
go build -o stickerbot-bot ./cmd/bot
EOF
sudo systemctl restart stickerbot.service
```

## Важные замечания
- Бот должен быть администратором канала, чтобы `GetChatMember` работал и бонусы выдавались.
- В `SUBSCRIPTION_CHANNEL_USERNAME` указывайте без `@`; для приватного канала используйте `SUBSCRIPTION_CHANNEL_ID`.
- Следите за валидностью `MYSQL_DSN`, токенов Telegram и ключа KIE.
