# Бэкенд: drawingService и «Крокодил»

[Оглавление документации](README.md)

Микросервис хранения PNG-рисунков админки и самостоятельный API игры «Крокодил».

Нужна версия Go, указанная в [go.mod](../go.mod). Команды ниже выполняются из корня `drawingService`, если не указан фронтенд.

- Хранит PNG-файлы в Google Drive через service account или OAuth refresh token.
- Хранит metadata в локальной bbolt-БД.
- Старое API проверяет сервисный токен и заголовки пользователя; игровое API использует отдельные гостевые токены.
- Прежний сервис по умолчанию слушает `127.0.0.1:8090`; самостоятельный игровой API — `127.0.0.1:8091`. Production-настройки описаны в [документации деплоя](deployment.md).

## Локальный запуск

1. Скопировать `.env.example` в `.env` и заполнить:
   - `DRAWING_SERVICE_TOKEN` — общий токен с основным backend.
   - `GOOGLE_DRIVE_FOLDER_ID` — ID папки Google Drive.
   - `GOOGLE_DRIVE_AUTH_MODE` — `service_account` или `oauth`.
   - для `service_account`: `GOOGLE_SERVICE_ACCOUNT_JSON_PATH` — путь к JSON-ключу service account.
   - для `oauth`: `GOOGLE_OAUTH_CLIENT_ID`, `GOOGLE_OAUTH_CLIENT_SECRET`, `GOOGLE_OAUTH_REFRESH_TOKEN`.
2. Если выбран `service_account`, положить JSON в `secrets/google-service-account.json`.
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

Пример service unit `/etc/systemd/system/drawing-service.service` для установки в `/var/go/drawing` (фактические пути существующего сервера отличаются):

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

## Игра «Крокодил»

От 3 до 12 человек, каждый со своего телефона или компьютера. Комната по приглашению, готовность всех, чередование фраз и рисунков, совместный просмотр историй. Каждый рисует каждую тему: для N игроков партия состоит из 2N этапов. Таймера нет, следующий этап начинается после всех ответов. При отключении игрока партия ждёт; хозяин может прервать её кнопкой новой игры.

Фронтенд находится в соседнем репозитории `web-crocodile`. Для локальной игры без Google Drive и настроек существующего сервиса:

```bash
# В drawingService
go run ./cmd/crocodile -addr 127.0.0.1:8091 -db crocodile.db
```

```bash
# В web-crocodile, второй терминал
npm ci
npm run dev
```

Открыть `http://localhost:5173` на компьютере. На других устройствах в той же Wi-Fi-сети открыть `http://<LAN-IP-компьютера>:5173`, создать комнату и скопировать приглашение с этого адреса. `localhost` в ссылке работает только на самом компьютере. Vite проксирует API на `127.0.0.1:8091`; прямой доступ устройств к порту Go не нужен.

Для друзей в интернете нужен размещённый сайт с HTTPS и общедоступный сервер. Собрать фронтенд `npm run build`, раздавать `dist/`, направлять `/api/game/` на Go с того же origin. При использовании основного `drawing-service` целевой порт — его существующий порт (по умолчанию 8090); `GAME_API_TARGET` на фронтенде меняет адрес dev-прокси. Старые `/internal/drawing/*` должны остаться за существующим gateway с `X-Service-Token`.

Основная команда `cmd/drawing-service` автоматически подключает игру на `/api/game/*`. Старые методы рисунков, штампов, авторизация и `/healthz` сохранены. Для игры используются новые buckets `crocodile_rooms_v1` и `crocodile_meta_v1` в bbolt; игровое API не обращается в Google Drive и не меняет галерею.

### Контракт API

Все тела запросов — JSON; ошибки — `{ "error": "описание" }`. Кроме создания и входа, нужен `Authorization: Bearer <token>`. Токен выдаётся только вошедшему участнику и не включается в приглашение.

| Метод | Путь после `/api/game` | Тело / результат |
| --- | --- | --- |
| POST | `/rooms` | `{name}` → 201 `{token, room}` |
| POST | `/rooms/{code}/join` | `{name}` → 201 `{token, room}` |
| GET | `/rooms/{code}` | Личное состояние, игроки и текущее задание |
| POST | `/rooms/{code}/ready` | `{ready: true/false}` → состояние |
| POST | `/rooms/{code}/submit` | `{gameId, stage, text}` либо `{gameId, stage, image}` → состояние |
| GET | `/rooms/{code}/images/{id}` | PNG; только своё задание или финальные результаты |
| POST | `/rooms/{code}/restart` | `{gameId}` → лобби, только хозяин; удаляет старые истории |
| POST | `/rooms/{code}/leave` | `{}` → 204; только до старта или после финала |

`stage` начинается с 0; чётный этап — текст, нечётный — рисунок. `image` — base64 PNG без префикса data URL. Повтор уже принятого ответа не добавляет ход; повтор старой партии получает 409. Состояние содержит `version` для защиты клиента от ответов не по порядку. До финала сервер не передаёт полные цепочки или чужие токены. Опрос примерно раз в секунду; PNG загружаются отдельными запросами.

### Хранение и ограничения

В production игровые PNG находятся внутри bbolt-файла `/var/lib/crocodile/game.db`, в отдельном вложенном bucket `images` каждой комнаты. Это не файлы Google Drive. Systemd хранит базу вне релизов, фактический каталог — `/var/lib/private/crocodile`.

В браузере сессия хранится в `localStorage` под ключом `crocodile-session`, имя — `crocodile-name`, черновик текущего этапа — `crocodile-draft:<room>:<gameId>:<stage>`. Черновик удаляется после успешной отправки; брошенный черновик может остаться на устройстве. Очистка серверных комнат не очищает localStorage.

- Один процесс и локальная bbolt. Состояние и PNG сохраняются атомарно и восстанавливаются после перезапуска.
- До 64 комнат; 256 МиБ суммарного объёма игровых PNG. Неактивные 24 часа комнаты удаляются с рисунками; активность — успешное игровое действие, не фоновый опрос. Очистка выполняется при запуске и раз в минуту. bbolt повторно использует освобождённое место, файл не обязан сразу уменьшаться.
- Имя 1–24 символа, текст 1–200; PNG до 512 КиБ, сторона до 2048 пикселей, площадь до 2 мегапикселей. Холст сайта — 1024×768.
- До 30 созданий/входов в минуту на адрес прямого подключения. За reverse proxy лимит общий для прокси; при большем трафике настройте ограничения на внешнем сервере и пересмотрите этот предел.
- Сессия и текущие черновики хранятся в браузере. Для независимых игроков нужны разные устройства или отдельные профили браузера. Восстановление после очистки данных браузера не поддерживается.

Проверки: `go test -race ./...` включает старые маршруты с подключённой игрой, партии для чётного/нечётного числа участников, конфиденциальность изображений, повторные/конкурентные отправки, восстановление базы, переполнение хранилища и очистку комнат.
