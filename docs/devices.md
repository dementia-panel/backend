# User devices API

All administrative routes below use the `/api/admin` prefix. They accept an administrator session or an external API bearer token. A user reference can be a UUID or a positive Telegram ID, for example `/users/550e8400-e29b-41d4-a716-446655440000/devices` or `/users/123456789/devices`. An unknown reference returns `404`; an ambiguous Telegram ID returns `409` and requires a UUID.

A device is a registered set of credentials, with one independently generated VPN key pair per subscribed node. The same device is counted once across all nodes. This is a count of issued access credentials, not verified physical hardware.

## Device management

- `GET /users/{user}/devices`: list devices with `id`, `user_id`, `name`, `created_at`, `updated_at`, `last_connected_at`, and `online`.
- `GET /users/{user}/devices/{device_id}`: read one owned device, or return `404`.
- `POST /users/{user}/devices`: create a device and provision its node keys. Body: `{"name":"Work laptop"}`. Returns `201` and the device; returns `409` if the user's limit is reached.
- `PATCH /users/{user}/devices/{device_id}`: rename a device without changing its keys. Body: `{"name":"Personal laptop"}`.
- `DELETE /users/{user}/devices/{device_id}`: revoke only this device on all nodes after synchronization. Subscription server assignments and other devices are preserved.
- `GET /users/{user}/devices/{device_id}/nodes/{node_id}/profile`: retrieve the existing device profile as `{"conf":"...","key":"vpn://..."}`. This does not issue another device or regenerate keys.
- `GET /subscriptions/{subscription_id}/nodes/{node_id}/profile?device_id={device_id}`: export a particular device through the existing administrator profile endpoint. Omitting `device_id` keeps compatibility by selecting the earliest remaining device.

Device names contain 1–128 UTF-8 bytes and no control characters. Creating and deleting devices is transactional; concurrent requests cannot exceed the issuance limit. Deleted keys are removed by the next successful node synchronization. Renaming does not restart the VPN.

## Limits

- `GET /settings/devices`: read `{"default_device_limit":3}`.
- `PUT /settings/devices`: save `{"default_device_limit":5}` or `{"default_device_limit":null}` for unlimited access. Only subsequently created users inherit the setting.
- `PUT /users/{user}/device-limit`: save `{"device_limit":2}` or `{"device_limit":null}` to disable the limit for this user.

Limits must be integers between 1 and 10000 or `null`. Reducing a limit preserves existing devices and blocks further issuance until the count is below the new limit. Changing the global setting does not overwrite individual user settings. All settings persist in PostgreSQL; no new environment variables are required.

User detail and list responses include `device_limit`, `device_count`, and `online_device_count`. Online status is an estimate based on a handshake within three minutes and a recent synchronized node report. UUID and Telegram references also work for user detail, updates, deletion, subscription assignment, device limits and device exports. Bulk user actions resolve their `ids` values the same way. Traffic filters accept `user_id` as either reference, or a separate `telegram_id` query parameter.

## Existing subscriptions and public profiles

Migration `000022_user_devices` assigns existing peer keys to one `Main device` per user, without replacing private keys or changing tunnel addresses. Existing users retain unlimited device issuance until a limit is explicitly set. On a user's first new subscription, one initial device is created if device management has never been initialized; removing every device does not silently reissue one.

The public subscription manifest includes `devices` (`id`, `name`). Individual device profiles are available under:

- `/s/{token}/devices/{device_id}/nodes/{node_id}.vpn`
- `/s/{token}/devices/{device_id}/nodes/{node_id}.conf`
- `/s/{token}/devices/{device_id}/nodes/{node_id}.key`

The subscription page supports `?device_id={device_id}` and a device selector. Existing unscoped profile URLs remain compatible with the earliest remaining device. A subscription token authorizes access to its user's device profiles; knowing a device UUID alone grants no access. An exported profile remains usable while its user, subscription and device are active and their keys are synchronized.

Device events use the `user` webhook group: `user.device_created`, `user.device_updated` and `user.device_deleted`. Payloads contain device and user identifiers and the device name, never private keys.
