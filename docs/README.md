# Документация «Крокодила»

- [Бэкенд и правила игры](backend.md) — две Go-команды, запуск, API, раунды, хранение рисунков, ограничения и тесты.
- [Деплой и обслуживание](deployment.md) — GitHub Actions, секреты, VPS, Nginx, systemd, очистка релизов и диагностика.
- [Документация фронтенда](https://github.com/EvgenyGulyaev/web-crocodile/tree/main/docs) — клиент, холст, локальный запуск и проверка партии.

Рабочий сайт: [draw.wedding-en.ru](https://draw.wedding-en.ru/). В [админке](https://admin.wedding-en.ru/dashboard) карточка `crocodile` относится к игре, `drawyer` — к прежнему `drawing-service`. Карточки показывают состояние процесса systemd; это не проверка доступности Google Drive.

## История проектирования

Эти документы фиксируют исходный замысел и ход работ; актуальные правила эксплуатации находятся выше.

- [Исходные требования](superpowers/specs/2026-09-06-crocodile-design.md).
- [План реализации игры](superpowers/plans/2026-09-06-crocodile.md).
- [План настройки GitHub Actions](superpowers/plans/2026-09-06-github-deploy.md).
