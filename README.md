[Сайт документации](https://dementia-panel.github.io/)

<a href="#english"><kbd>English</kbd></a> <a href="#русский"><kbd>Русский</kbd></a>

# dementia backend

## English

The Go API manages users, subscriptions, and AmneziaWG nodes. It also handles node provisioning and synchronization. [Full installation guide](https://github.com/dementia-panel#english) · [API documentation](docs/API.md#english)

Webhook requests include `X-Dementia-Signature` (hex HMAC-SHA256 of the JSON body) and `X-Dementia-Timestamp` (UTC RFC 3339). The `user.first_connected` event is emitted once per user when their first VPN handshake is recorded.

## Русский

API на Go управляет пользователями, подписками и нодами AmneziaWG. Он устанавливает ноды и синхронизирует их настройки. [Полная инструкция по установке](https://github.com/dementia-panel#русский) · [Документация API](docs/API.md#russian)

Запросы вебхука содержат `X-Dementia-Signature` (HMAC-SHA256 от JSON-тела в hex) и `X-Dementia-Timestamp` (UTC RFC 3339). Событие `user.first_connected` отправляется один раз для каждого пользователя после первого зарегистрированного VPN-рукопожатия.
