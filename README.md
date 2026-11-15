# dementia backend

Backend управляет пользователями, подписками и нодами. Docker Compose запускает API, PostgreSQL и Valkey. Миграции базы применяются при старте backend.

## Установка backend

Нужны Docker с Compose v2 и OpenSSL для генерации ключей. Команды выполняются из каталога backend, где лежит `compose.yaml`. При первой установке создай файл настроек:

```sh
umask 077
cp .env.example .env
```

В `.env` замени `POSTGRES_PASSWORD=CHANGE_ME` на уникальный пароль. Дважды выполни команду ниже и сохрани разные результаты в `JWT_SIGNING_KEY` и `ENCRYPTION_KEY`.

```sh
openssl rand -base64 32
```

Укажи `PUBLIC_URL` как точный HTTPS-адрес, по которому будет открываться панель, без пути. Для локальной установки из [инструкции панели](../panel/README.md) используй `https://localhost:8443`. Для удалённого доступа укажи свой адрес, например `https://panel.example.com`. При запуске через Compose адреса PostgreSQL и Valkey задаются в `compose.yaml`.

```sh
docker compose up -d --build --wait
curl -fsS http://127.0.0.1:8080/readyz
```

Если изменил `BACKEND_PORT`, подставь его в команду проверки. API слушает HTTP на локальном порту. Панель и HTTPS настраиваются отдельно по [инструкции панели](../panel/README.md).

## Подключение нод

- Backend должен иметь доступ к SSH-порту VPS. Ноде нужен исходящий HTTPS-доступ к GitHub для установки и к `PUBLIC_URL` для синхронизации с панелью.
- Для VPS укажи в `PUBLIC_URL` доступный с него адрес с сертификатом, которому доверяет нода. `localhost` подходит для локальной панели в браузере, но с VPS этот адрес указывает на сам VPS.
- После настройки HTTPS проверь с VPS, что `curl -fsS https://panel.example.com/readyz` проходит. Затем открой раздел «Ноды» и добавь сервер. Установщик берёт файлы из [последнего релиза ноды](https://github.com/dementia-panel/node/releases/latest).

## Обновление и диагностика

После обновления исходников повтори `docker compose up -d --build --wait`. Логи API доступны через `docker compose logs -f backend`. Для восстановления сохраняй резервную копию PostgreSQL и `.env`, особенно `ENCRYPTION_KEY`, которым защищены данные в базе.
