# Размещение draw.wedding-en.ru

[Оглавление документации](README.md)

Файлы установки остаются в [deploy](../deploy/): [скрипт выкладки](../deploy/deploy-crocodile.sh), [systemd unit](../deploy/crocodile.service), [Nginx](../deploy/draw.conf), [правило sudo](../deploy/crocodile-deploy.sudoers).

## GitHub Actions

В Go-репозитории две сборки и два независимых workflow:

- `.github/workflows/deploy.yml`: прежний `cmd/drawing-service`, существующие секреты `VPS_HOST`, `VPS_USER`, `VPS_PORT`, `VPS_SSH_KEY`, `VPS_TARGET`. Версия Go берётся из `go.mod`; тесты выполняются перед сборкой. Binary загружается как `drawing-service.next`, затем атомарно заменяется; `drawing-service.previous` остаётся только до успешной проверки выкладки и затем удаляется. Остановленный сервис остаётся остановленным.
- `.github/workflows/deploy-crocodile.yml`: `cmd/crocodile`, отдельный `crocodile.service`, каталог `/opt/crocodile`. Автоматический запуск при изменении игрового API/зависимостей в `main`, ручной запуск через Actions.
- В репозитории `web-crocodile` свой `.github/workflows/deploy.yml`: Node.js 22, `npm ci`, `npm run build`, публикация статики после push в `main`.

Два игровых workflow используют secrets `CROCODILE_SSH_KEY` и `CROCODILE_HOST_FINGERPRINT`. Ключ относится только к серверному пользователю `crocodile-deploy`; sudo разрешает ему только `/usr/bin/systemctl restart crocodile.service`. Личный SSH-ключ и root-ключ в игровые workflow не передаются. Проверенный fingerprint сервера: `SHA256:HK5ezQyo+xDXzJMim9jDhsM4NA/ROYEAZyJIxyH50cs`.

`deploy-crocodile.sh` установлен как `/usr/local/bin/deploy-crocodile`, sudoers — `/etc/sudoers.d/crocodile-deploy`. Код этих файлов обновляется отдельно администратором, автоматически из workflow не устанавливается.

Загрузка идёт в `/opt/crocodile/incoming/<api|web>-<run>-<attempt>-<sha>`. Скрипт удерживает общий `flock` между двумя репозиториями, сохраняет неизменяемый компонент и атомарно переключает `current`. API проверяется по ответу 404 на несуществующую комнату, frontend — сравнением HTML через локальный HTTPS vhost. При ошибке возвращается предыдущая ссылка, неудачный релиз и его загрузка удаляются. Фронтенд не требует перезапуска Nginx или API: неизменяемый бинарник связан hard link, поэтому работающий процесс не удерживает отдельную удалённую копию. База `/var/lib/crocodile/game.db` не копируется и не заменяется.

После успешной проверки остаётся только текущий релиз: все остальные каталоги в `/opt/crocodile/releases` удаляются под тем же lock. При выкладке фронтенда старые assets не переносятся; открытой вкладке может потребоваться обновление страницы. Загрузки другого параллельного деплоя не удаляются. Изолированная проверка очистки и отката на Linux: `python3 deploy/test-deploy.py`. Статическая проверка: `shellcheck deploy/deploy-crocodile.sh` и `actionlint`.

Настройка завершена 6 сентября 2026: секреты добавлены в оба игровых репозитория, все три workflow успешно выполнены из `main` (ссылки в `docs/superpowers/plans/2026-09-06-github-deploy.md`). Пользователь игры имеет отдельные UID/GID `49000`: UID `1001` уже встречается у артефактов старых сервисов, повторно использовать его нельзя.

При финальной проверке старый `drawing-service` на `127.0.0.1:8086` был запущен, но `/healthz` вернул `503`: Google OAuth `invalid_grant`. Его Google-код и credentials этим изменением не менялись; доступ к Drive требует отдельного восстановления. По журналу сервис был запущен в 16:52 UTC до выполнения шага нового workflow в 16:56 UTC; workflow перезапустил уже работающий сервис.

Размещено 6 сентября 2026 года на `95.181.224.178`. Сайт: https://draw.wedding-en.ru.

## Что установлено

- Отдельный сервис `crocodile.service`, включён автозапуск. Запускает только `cmd/crocodile`, не требует Google Drive и не заменяет основной drawing-service.
- API слушает только `127.0.0.1:8091`; браузер обращается к нему через `/api/game/` на том же HTTPS-домене.
- Текущая версия: `/opt/crocodile/current` — символьная ссылка на единственный успешный релиз в `/opt/crocodile/releases/`. Бинарный файл `crocodile`, фронтенд в `web/`. Первоначальный ручной релиз `20260906-1600` удалён после перехода на автоматический деплой.
- База: `/var/lib/crocodile/game.db`. Systemd `DynamicUser` и `StateDirectory` сохраняют её между перезапусками; фактическое хранение может быть в `/var/lib/private/crocodile`.
- Ограничения процесса: 128 МиБ памяти, 25% одного CPU, 64 задачи. Go: `GOMEMLIMIT=64MiB`, `GOMAXPROCS=1`.
- Новый Nginx vhost: `/etc/nginx/sites-available/draw.conf`, ссылка в `sites-enabled`. Остальные конфигурации не изменены. POST ограничены по IP; GET рисунков не попадают под этот лимит, чтобы финал на 12 игроков мог загрузить все изображения.
- Отдельный сертификат: `/etc/letsencrypt/live/draw.wedding-en.ru/`, действителен до 5 декабря 2026 года. HTTP перенаправляется на HTTPS, кроме HTTP-01 challenge.
- Продление использует уже существующий включённый `certbot-ip-renew.timer` и `/opt/certbot/bin/certbot`. Его конфигурация не менялась. Сертификат игры добавлен как отдельная renewal-запись с webroot `/var/www/certbot`.
- Резервная копия исходной конфигурации Nginx: `/root/crocodile-deploy-backup-20260906/nginx`.

## История: проверено при первоначальном размещении 6 сентября 2026

- `nginx -t`, проверка systemd unit, запущенный и включённый сервис.
- HTTPS: 200, проверка цепочки сертификата успешна; HTTP: 301 на HTTPS.
- Полная браузерная партия на публичном домене в трёх независимых сессиях: все 6 этапов, сенсорный ввод, восстановление текста и рисунка после перезагрузки, отмена штриха, финальные истории, переход роли хозяина, новая игра и восстановление после недействительной сессии. Ошибок JavaScript нет.
- SHA-256 установленного бинарного файла, index.html, Nginx vhost и unit совпадают с подготовленными локальными файлами.
- Все исходные файлы `/etc/nginx` побайтово совпадают с резервной копией; добавлены только `draw.conf` и ссылка на него. PID главного процесса Nginx остался `2879164`: использовался reload, не restart.
- После браузерной проверки память игры около 10 МиБ, `NRestarts=0`, событий OOM в журнале ядра за время размещения нет.
- `certbot renew --cert-name draw.wedding-en.ru --dry-run --no-random-sleep-on-renew` выполнен успешно.

## История: соседние сайты при первоначальном размещении

| Сайт | До | После |
| --- | --- | --- |
| `https://wedding-en.ru/` | 200 | 200 |
| `https://www.wedding-en.ru/` | 200 | 200 |
| `https://admin.wedding-en.ru/` | 200 | 200 |
| `https://scheduler.publicvm.com/` | 502 | 502 |
| `https://www.scheduler.publicvm.com/` | 301 на scheduler | 301 на scheduler |
| HTTP vhost `lawyer.scheduler.publicvm.com` | 502 | 502 |
| HTTP vhost `wedding.scheduler.publicvm.com` | 301 на wedding-en.ru | 301 на wedding-en.ru |

`bot.service` уже до запуска игры завершался с `status=1/FAILURE` и автоматически перезапускался примерно каждые 15 секунд. Это подтверждается журналом systemd с 15:58 UTC, до изменений; счётчик перезапусков превышал 23 тысячи. Сервис бота и его конфигурация не изменялись. Диагностика/исправление этих прежних проблем не входили в размещение игры.

## Обслуживание

На сервере:

```sh
systemctl status crocodile --no-pager
journalctl -u crocodile -n 50 --no-pager
nginx -t
```

Обычная выкладка — push в `main` соответствующего репозитория. Для повторной выкладки без изменения кода доступны ручные запуски:

```sh
gh workflow run deploy-crocodile.yml -R EvgenyGulyaev/drawingService --ref main
gh workflow run deploy.yml -R EvgenyGulyaev/web-crocodile --ref main
```

Текущий путь и объём релиза:

```sh
readlink -f /opt/crocodile/current
du -sh /opt/crocodile/releases
```

Старый релиз доступен для автоматического отката только во время проверки нового. После успешной выкладки он удаляется; вернуть более ранний код можно новой сборкой нужного состояния Git. Скрипт выкладки обновляется отдельно администратором из `deploy/deploy-crocodile.sh`.

Базу не копировать поверх существующей. Для резервной копии работающей bbolt использовать транзакционный snapshot; обычную копию файла делать при остановленном только игровом сервисе.

## Отключение только новой игры

Сохранить существующие файлы и базу, отключить только добавленный vhost и сервис:

```sh
mv /etc/nginx/sites-enabled/draw.conf /root/crocodile-deploy-backup-20260906/draw.enabled
if nginx -t; then
    systemctl reload nginx
    systemctl disable --now crocodile.service
else
    mv /root/crocodile-deploy-backup-20260906/draw.enabled /etc/nginx/sites-enabled/draw.conf
    exit 1
fi
```

Не восстанавливать весь `/etc/nginx` поверх текущего каталога: это могло бы затереть последующие изменения других сайтов. Сертификаты, старые сервисы и их конфигурации для отката игры менять не требуется.

## Последующее удаление старых сайтов — 6 сентября 2026

По отдельному запросу владельца отключены `scheduler.publicvm.com` (включая `www`) и `lawyer.scheduler.publicvm.com`.

- Их прежние server-блоки удалены из активных и сохранённых конфигураций `bot.conf`, `bot-ssl.conf`, `lawyer.conf`. Блоки админки, IP и порта 8443 сохранены без изменений.
- `retired-publicvm.conf` возвращает 410 для HTTP и отклоняет TLS-handshake для этих имён: запросы не попадают на другой сайт по умолчанию.
- Неиспользуемый сертификат `scheduler.publicvm.com` и его автопродление удалены через Certbot. Сертификаты IP, admin, wedding и draw сохранены.
- `draw.wedding-en.ru`, `wedding-en.ru`, `www.wedding-en.ru`, `admin.wedding-en.ru` проверены: HTTPS 200. Главные процессы Nginx и работающих приложений не перезапускались.
- DNS-записи и данные старых приложений не удалялись. `wedding.scheduler.publicvm.com` и доступ по IP:8443 остались отдельными адресами с прежним поведением.
- Резервные копии конфигураций и сертификатов: `/root/retired-sites-backup-20260906/`. Это более поздняя копия, чем резервная копия первоначального размещения игры.
