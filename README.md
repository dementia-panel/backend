# dementia backend

Backend управляет пользователями, подписками и нодами. Docker Compose запускает API, PostgreSQL и Valkey. Миграции базы применяются при старте backend.

## Установка из Docker Hub

Готовый Compose запускает backend, панель, PostgreSQL и Valkey. На сервере нужны Docker с Compose v2 и сертификат для HTTPS.

```sh
mkdir dementia && cd dementia
umask 077
curl -fsSLO https://raw.githubusercontent.com/dementia-panel/backend/v0.1.0/deploy/compose.yaml
curl -fsSL https://raw.githubusercontent.com/dementia-panel/backend/v0.1.0/deploy/.env.example -o .env
```

Заполни `.env`. Укажи уникальный `POSTGRES_PASSWORD`, сгенерируй отдельно `JWT_SIGNING_KEY` и `ENCRYPTION_KEY` командой `openssl rand -base64 32` и проверь `PUBLIC_URL`. Адрес должен точно совпадать с HTTPS-адресом, который открываешь в браузере.

Создай каталог `tls/` рядом с `compose.yaml`. Помести цепочку сертификатов в `tls/localhost.crt`, а приватный ключ в `tls/localhost.key`. Эти имена ожидает образ панели. Для локального запуска можно сгенерировать [сертификат для localhost](https://github.com/dementia-panel/panel/tree/v0.1.0#локальный-запуск). При настройках по умолчанию открой `https://localhost:8443`. Для доступа с VPS настрой HTTPS на адресе, достижимом с ноды. Порт панели по умолчанию доступен через loopback и подходит для прокси на том же сервере. Для прямого доступа извне задай `PANEL_BIND` и `PANEL_PORT` и укажи соответствующий внешний адрес в `PUBLIC_URL`.

```sh
docker compose up -d --pull always --wait
```

На чистой базе интерфейс предложит создать администратора. Образы [backend](https://hub.docker.com/r/bor333/dementia-backend) и [panel](https://hub.docker.com/r/bor333/dementia-panel) закреплены в Compose на версии `0.1.0`. Для обновления замени теги и повтори команду запуска, сохранив `.env` и тома базы.

## Сборка backend из исходников

Нужны Docker с Compose v2 и OpenSSL для генерации ключей. Команды выполняются из каталога backend, где лежит `compose.yaml`. При первой установке создай файл настроек:

```sh
umask 077
cp .env.example .env
```

В `.env` замени `POSTGRES_PASSWORD=CHANGE_ME` на уникальный пароль. Дважды выполни команду ниже и сохрани разные результаты в `JWT_SIGNING_KEY` и `ENCRYPTION_KEY`.

```sh
openssl rand -base64 32
```

Укажи `PUBLIC_URL` как точный HTTPS-адрес, по которому будет открываться панель, без пути. Для локальной установки используй `https://localhost:8443`. Для удалённого доступа укажи свой адрес, например `https://panel.example.com`. При запуске через Compose адреса PostgreSQL и Valkey задаются в `compose.yaml`.

```sh
docker compose up -d --build --wait
curl -fsS http://127.0.0.1:8080/readyz
```

Если изменил `BACKEND_PORT`, подставь его в команду проверки. API слушает HTTP на локальном порту. Панель и HTTPS настраиваются отдельно по [инструкции панели](https://github.com/dementia-panel/panel).

## Подключение нод

- Backend должен иметь доступ к SSH-порту VPS. Ноде нужен исходящий HTTPS-доступ к GitHub для установки и к `PUBLIC_URL` для синхронизации с панелью.
- Для VPS укажи в `PUBLIC_URL` доступный с него адрес с сертификатом, которому доверяет нода. `localhost` подходит для локальной панели в браузере, но с VPS этот адрес указывает на сам VPS.
- После настройки HTTPS проверь с VPS, что `curl -fsS https://panel.example.com/readyz` проходит. Затем открой раздел «Ноды» и добавь сервер. Установщик берёт файлы из [последнего релиза ноды](https://github.com/dementia-panel/node/releases/latest). Для самостоятельного развёртывания опубликован образ [dementia-node](https://hub.docker.com/r/bor333/dementia-node), которому требуется конфигурация ноды.

## Обновление и диагностика

После обновления исходников повтори `docker compose up -d --build --wait`. Логи API доступны через `docker compose logs -f backend`. Для восстановления сохраняй резервную копию PostgreSQL и `.env`, особенно `ENCRYPTION_KEY`, которым защищены данные в базе.
