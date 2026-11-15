<a href="#english"><kbd>English</kbd></a> <a href="#русский"><kbd>Русский</kbd></a>

# dementia backend

## English

### 1. Install Docker

If Docker is missing on a Linux test host, install Docker Engine with the Compose plugin:

```sh
curl -fsSL https://get.docker.com | sudo sh
```

For a production server, use Docker's package repository. If your Docker installation requires elevated permissions, run the Compose commands with `sudo`. [Docker Engine installation](https://docs.docker.com/engine/install/)

### 2. Download the deployment files

Create and enter the deployment directory. The next steps use curl and OpenSSL.

```sh
mkdir -p dementia
```

```sh
cd dementia
```

```sh
curl -fsSLO https://raw.githubusercontent.com/dementia-panel/backend/v0.1.0/deploy/compose.yaml
```

```sh
curl -fsSL https://raw.githubusercontent.com/dementia-panel/backend/v0.1.0/deploy/.env.example -o .env
```

### 3. Fill in `.env`

| Variable | Value |
| --- | --- |
| `PUBLIC_URL` | The exact HTTPS address of the panel, with no path or trailing slash. Use `https://localhost:8443` for local use, or an address reachable from your nodes for remote use. |
| `PANEL_BIND` | Keep `127.0.0.1` for local access or a reverse proxy on the same host. Set the host's listening IP, or `0.0.0.0`, for direct external access. |
| `PANEL_PORT` | Host port for the panel. The default is `8443`. Match the port in `PUBLIC_URL` when connecting directly. |
| `POSTGRES_USER`, `POSTGRES_DB` | Keep `backend` for both unless you need different names. |
| `POSTGRES_PASSWORD` | Replace `CHANGE_ME` with a unique password. |
| `JWT_SIGNING_KEY`, `ENCRYPTION_KEY` | Generate a separate 32-byte Base64 key for each field. Keep the encryption key with your database backups. |
| `LOG_LEVEL`, `NODE_PING_INTERVAL_SECONDS` | Defaults: `info` and `30`. |
| `WEBHOOK_URL`, `WEBHOOK_SECRET_HEADER` | Leave empty unless using a webhook. Set both to enable it. |
| `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID` | Set both to enable Telegram. `TELEGRAM_THREAD_ID` is optional. |
| `TELEGRAM_MODE`, `TELEGRAM_WEBHOOK_SECRET` | Keep `polling` by default. Set a secret if you select `webhook`. |

Generate a PostgreSQL password:

```sh
openssl rand -hex 32
```

Run the next command twice, once for each key:

```sh
openssl rand -base64 32
```

Keep `.env` outside version control.

### 4. Start the backend

Compose starts PostgreSQL and Valkey as dependencies. The backend applies database migrations on startup.

```sh
docker compose up -d --pull always --wait backend
```

### 5. Start the panel

Continue with the panel setup in the same deployment folder. [Panel installation](https://github.com/dementia-panel/panel#english)

## Русский

### 1. Установи Docker

Если на тестовой Linux-машине ещё нет Docker, установи Docker Engine с плагином Compose:

```sh
curl -fsSL https://get.docker.com | sudo sh
```

Для рабочего сервера используй репозиторий пакетов Docker. Если твоя установка Docker требует повышенных прав, запускай команды Compose через `sudo`. [Установка Docker Engine](https://docs.docker.com/engine/install/)

### 2. Скачай файлы установки

Создай папку для установки и перейди в неё. Для следующих шагов нужны curl и OpenSSL.

```sh
mkdir -p dementia
```

```sh
cd dementia
```

```sh
curl -fsSLO https://raw.githubusercontent.com/dementia-panel/backend/v0.1.0/deploy/compose.yaml
```

```sh
curl -fsSL https://raw.githubusercontent.com/dementia-panel/backend/v0.1.0/deploy/.env.example -o .env
```

### 3. Заполни `.env`

| Переменная | Значение |
| --- | --- |
| `PUBLIC_URL` | Точный HTTPS-адрес панели без пути и завершающего слеша. Для локального запуска используй `https://localhost:8443`, для удалённого — адрес, доступный с нод. |
| `PANEL_BIND` | Для локального доступа или прокси на том же хосте оставь `127.0.0.1`. Для прямого внешнего доступа укажи IP хоста или `0.0.0.0`. |
| `PANEL_PORT` | Порт панели на хосте, по умолчанию `8443`. При прямом подключении он должен совпадать с портом в `PUBLIC_URL`. |
| `POSTGRES_USER`, `POSTGRES_DB` | Оставь `backend` в обоих полях, если другие имена не нужны. |
| `POSTGRES_PASSWORD` | Замени `CHANGE_ME` на уникальный пароль. |
| `JWT_SIGNING_KEY`, `ENCRYPTION_KEY` | Сгенерируй для каждого поля отдельный 32-байтовый ключ в Base64. Храни ключ шифрования вместе с резервными копиями базы. |
| `LOG_LEVEL`, `NODE_PING_INTERVAL_SECONDS` | Значения по умолчанию — `info` и `30`. |
| `WEBHOOK_URL`, `WEBHOOK_SECRET_HEADER` | Оставь пустыми, если вебхук не нужен. Для подключения заполни оба поля. |
| `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID` | Заполни оба поля для подключения Telegram. `TELEGRAM_THREAD_ID` необязателен. |
| `TELEGRAM_MODE`, `TELEGRAM_WEBHOOK_SECRET` | По умолчанию оставь `polling`. Если выбран `webhook`, укажи секрет. |

Сгенерируй пароль PostgreSQL:

```sh
openssl rand -hex 32
```

Следующую команду выполни дважды, по одному разу для каждого ключа:

```sh
openssl rand -base64 32
```

Храни `.env` вне Git.

### 4. Запусти backend

Compose автоматически запустит PostgreSQL и Valkey. Backend применит миграции базы при старте.

```sh
docker compose up -d --pull always --wait backend
```

### 5. Запусти панель

Продолжи настройку панели в той же папке установки. [Установка панели](https://github.com/dementia-panel/panel#русский)
