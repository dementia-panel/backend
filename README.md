<a href="#english"><kbd>English</kbd></a> <a href="#русский"><kbd>Русский</kbd></a>

# dementia backend

## English

### 1. Install Docker

On the deployment server, configure the official Docker APT repository for your distribution, then install Docker Engine and the Compose plugin. [Ubuntu](https://docs.docker.com/engine/install/ubuntu/#install-using-the-apt-repository) · [Debian](https://docs.docker.com/engine/install/debian/#install-using-the-apt-repository)

```sh
sudo apt install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
```

### 2. Download the deployment files

Create and enter the deployment directory. You will need curl, OpenSSL, and a domain pointing to this server.

```sh
mkdir -p dementia
```

```sh
cd dementia
```

```sh
curl -fsSLO https://raw.githubusercontent.com/dementia-panel/backend/main/deploy/compose.yaml
```

```sh
curl -fsSL https://raw.githubusercontent.com/dementia-panel/backend/main/deploy/.env.example -o .env
```

### 3. Fill in `.env`

| Variable | Value |
| --- | --- |
| `PUBLIC_URL` | The public HTTPS address of the panel, for example `https://panel.example.com`. Use a domain reachable from your nodes and omit the trailing slash. |
| `PANEL_BIND` | Set `0.0.0.0` to publish HTTPS on the server’s IPv4 interfaces. |
| `PANEL_PORT` | Set `443` for standard HTTPS. |
| `POSTGRES_USER`, `POSTGRES_DB` | Keep `backend` for both unless you need different names. |
| `POSTGRES_PASSWORD` | Replace `CHANGE_ME` with a unique password. |
| `JWT_SIGNING_KEY`, `ENCRYPTION_KEY` | Generate a separate 32-byte Base64 key for each field. Keep the encryption key with your database backups. |
| `LOG_LEVEL`, `NODE_PING_INTERVAL_SECONDS` | Defaults: `info` and `30`. |
| `WEBHOOK_URL`, `WEBHOOK_SECRET_HEADER` | Leave empty unless using a webhook. Set both to enable it. |
| `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID` | Set both to enable Telegram. `TELEGRAM_THREAD_ID` is optional. |
| `TELEGRAM_MODE`, `TELEGRAM_WEBHOOK_SECRET` | Keep `polling` by default. Set a secret if you select `webhook`. |

On a fresh installation, run these commands in the directory containing `.env`. Each writes a new value directly into the file. The two keys use standard Base64, including the trailing `=`.

```sh
postgres_password="$(openssl rand -hex 32)" && sed -i "s|^POSTGRES_PASSWORD=.*$|POSTGRES_PASSWORD=$postgres_password|" .env
```

```sh
jwt_key="$(openssl rand -base64 32)" && sed -i "s|^JWT_SIGNING_KEY=.*$|JWT_SIGNING_KEY=$jwt_key|" .env
```

```sh
encryption_key="$(openssl rand -base64 32)" && sed -i "s|^ENCRYPTION_KEY=.*$|ENCRYPTION_KEY=$encryption_key|" .env
```

If you set `WEBHOOK_URL`, generate the webhook secret:

```sh
webhook_secret="$(openssl rand -hex 32)" && sed -i "s|^WEBHOOK_SECRET_HEADER=.*$|WEBHOOK_SECRET_HEADER=$webhook_secret|" .env
```

If you select `TELEGRAM_MODE=webhook`, generate the Telegram webhook secret. Get `TELEGRAM_BOT_TOKEN` from BotFather.

```sh
telegram_webhook_secret="$(openssl rand -hex 32)" && sed -i "s|^TELEGRAM_WEBHOOK_SECRET=.*$|TELEGRAM_WEBHOOK_SECRET=$telegram_webhook_secret|" .env
```

Keep `.env` outside version control. For an existing installation, preserve the PostgreSQL password and encryption key unless you are deliberately rotating them.

### 4. Start the backend

Compose starts PostgreSQL and Valkey as dependencies. The backend applies database migrations on startup.

```sh
sudo docker compose up -d --pull always --wait backend
```

### 5. Start the panel

Continue with the panel setup in the same deployment folder. [Panel installation](https://github.com/dementia-panel/panel#english)

## Русский

### 1. Установи Docker

На сервере подключи официальный APT-репозиторий Docker для своего дистрибутива и установи Docker Engine с плагином Compose. [Ubuntu](https://docs.docker.com/engine/install/ubuntu/#install-using-the-apt-repository) · [Debian](https://docs.docker.com/engine/install/debian/#install-using-the-apt-repository)

```sh
sudo apt install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
```

### 2. Скачай файлы установки

Создай папку для установки и перейди в неё. Понадобятся curl, OpenSSL и домен, направленный на этот сервер.

```sh
mkdir -p dementia
```

```sh
cd dementia
```

```sh
curl -fsSLO https://raw.githubusercontent.com/dementia-panel/backend/main/deploy/compose.yaml
```

```sh
curl -fsSL https://raw.githubusercontent.com/dementia-panel/backend/main/deploy/.env.example -o .env
```

### 3. Заполни `.env`

| Переменная | Значение |
| --- | --- |
| `PUBLIC_URL` | Публичный HTTPS-адрес панели, например `https://panel.example.com`. Домен должен быть доступен с нод. Завершающий слеш не нужен. |
| `PANEL_BIND` | Укажи `0.0.0.0`, чтобы опубликовать HTTPS на IPv4-интерфейсах сервера. |
| `PANEL_PORT` | Укажи `443` для HTTPS. |
| `POSTGRES_USER`, `POSTGRES_DB` | Оставь `backend` в обоих полях, если другие имена не нужны. |
| `POSTGRES_PASSWORD` | Замени `CHANGE_ME` на уникальный пароль. |
| `JWT_SIGNING_KEY`, `ENCRYPTION_KEY` | Сгенерируй для каждого поля отдельный 32-байтовый ключ в Base64. Храни ключ шифрования вместе с резервными копиями базы. |
| `LOG_LEVEL`, `NODE_PING_INTERVAL_SECONDS` | Значения по умолчанию — `info` и `30`. |
| `WEBHOOK_URL`, `WEBHOOK_SECRET_HEADER` | Оставь пустыми, если вебхук не нужен. Для подключения заполни оба поля. |
| `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID` | Заполни оба поля для подключения Telegram. `TELEGRAM_THREAD_ID` необязателен. |
| `TELEGRAM_MODE`, `TELEGRAM_WEBHOOK_SECRET` | По умолчанию оставь `polling`. Если выбран `webhook`, укажи секрет. |

При первой установке выполни команды в папке с `.env`. Каждая команда записывает новое значение прямо в файл. Оба ключа записываются в стандартном Base64 вместе с завершающим знаком `=`.

```sh
postgres_password="$(openssl rand -hex 32)" && sed -i "s|^POSTGRES_PASSWORD=.*$|POSTGRES_PASSWORD=$postgres_password|" .env
```

```sh
jwt_key="$(openssl rand -base64 32)" && sed -i "s|^JWT_SIGNING_KEY=.*$|JWT_SIGNING_KEY=$jwt_key|" .env
```

```sh
encryption_key="$(openssl rand -base64 32)" && sed -i "s|^ENCRYPTION_KEY=.*$|ENCRYPTION_KEY=$encryption_key|" .env
```

Если указал `WEBHOOK_URL`, сгенерируй секрет вебхука:

```sh
webhook_secret="$(openssl rand -hex 32)" && sed -i "s|^WEBHOOK_SECRET_HEADER=.*$|WEBHOOK_SECRET_HEADER=$webhook_secret|" .env
```

Если выбрал `TELEGRAM_MODE=webhook`, сгенерируй секрет вебхука Telegram. `TELEGRAM_BOT_TOKEN` получи у BotFather.

```sh
telegram_webhook_secret="$(openssl rand -hex 32)" && sed -i "s|^TELEGRAM_WEBHOOK_SECRET=.*$|TELEGRAM_WEBHOOK_SECRET=$telegram_webhook_secret|" .env
```

Храни `.env` вне Git. На действующей установке сохраняй пароль PostgreSQL и ключ шифрования, если не проводишь их плановую замену.

### 4. Запусти backend

Compose автоматически запустит PostgreSQL и Valkey. Backend применит миграции базы при старте.

```sh
sudo docker compose up -d --pull always --wait backend
```

### 5. Запусти панель

Продолжи настройку панели в той же папке установки. [Установка панели](https://github.com/dementia-panel/panel#русский)
