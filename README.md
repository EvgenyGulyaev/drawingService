# drawingService

Микросервис хранения PNG-рисунков админки.

- Хранит PNG-файлы в Google Drive через service account.
- Хранит metadata в локальной bbolt-БД.
- Не имеет доступа к пользователям и не занимается авторизацией.
- Слушает только на `127.0.0.1:8090`, наружу не торчит.

## Локальный запуск

1. Скопировать `.env.example` в `.env` и заполнить:
   - `DRAWING_SERVICE_TOKEN` — общий токен с основным backend.
   - `GOOGLE_SERVICE_ACCOUNT_JSON_PATH` — путь к JSON-ключу service account.
   - `GOOGLE_DRIVE_FOLDER_ID` — ID папки Google Drive.
2. Положить JSON service account в `secrets/google-service-account.json`.
3. Запустить:

```bash
go run ./cmd/drawing-service
```

## Тесты

```bash
go test ./...
```

## Сборка production binary

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o drawing-service ./cmd/drawing-service
```

## Деплой

GitHub Actions workflow `.github/workflows/deploy.yml` собирает binary и копирует его на сервер по SCP.
После деплоя systemd перезапускает `drawing-service`.

Service unit `/etc/systemd/system/drawing-service.service`:

```ini
[Unit]
Description=Drawing storage service
After=network.target

[Service]
WorkingDirectory=/var/go/drawing
ExecStart=/var/go/drawing/drawing-service
Restart=always
RestartSec=5
EnvironmentFile=/var/go/drawing/.env

[Install]
WantedBy=multi-user.target
```

## Архитектура

```
Frontend -> /api/drawing/*
Existing backend (gateway) -> /internal/drawing/* (X-Service-Token)
drawingService -> Google Drive API v3
```
