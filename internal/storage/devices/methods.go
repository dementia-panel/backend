package devices

import (
	"context"
	"dementia/backend/internal/lib/logger/with"
	. "dementia/backend/internal/storage/shared"
	"dementia/backend/internal/vpn"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (s Repository) UserIDByTelegram(ctx context.Context, requestID string, telegramID int64) (string, error) {
	const op = "internal/storage/devices/UserIDByTelegram"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	rows, err := s.DB.Query(ctx, `SELECT id::text FROM users WHERE telegram_id=$1 ORDER BY id LIMIT 2`, telegramID)
	if err != nil {
		return "", err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "", pgx.ErrNoRows
	}
	if len(ids) > 1 {
		return "", ErrAmbiguousUser
	}
	return ids[0], nil
}
func (s Repository) DefaultDeviceLimit(ctx context.Context, requestID string) (*int, error) {
	const op = "internal/storage/devices/DefaultDeviceLimit"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	var limit *int
	err := s.DB.QueryRow(ctx, `SELECT default_device_limit FROM device_settings WHERE id`).Scan(&limit)
	return limit, err
}
func (s Repository) SetDefaultDeviceLimit(ctx context.Context, requestID string, limit *int) error {
	const op = "internal/storage/devices/SetDefaultDeviceLimit"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	_, err := s.DB.Exec(ctx, `UPDATE device_settings SET default_device_limit=$1 WHERE id`, limit)
	return err
}
func (s Repository) SetUserDeviceLimit(ctx context.Context, requestID string, userID string, limit *int) error {
	const op = "internal/storage/devices/SetUserDeviceLimit"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	tag, err := s.DB.Exec(ctx, `UPDATE users SET device_limit=$2,updated_at=now() WHERE id=$1`, userID, limit)
	if err == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return err
}
func (s Repository) Devices(ctx context.Context, requestID string, userID string) ([]Device, error) {
	const op = "internal/storage/devices/Devices"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	rows, err := s.DB.Query(ctx, SelectDevices+` WHERE d.user_id=$1 GROUP BY d.id ORDER BY d.created_at,d.id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.AppendRows([]Device{}, rows, pgx.RowToStructByPos[Device])
}
func (s Repository) EnsureInitialDevice(ctx context.Context, requestID string, tx pgx.Tx, userID string) error {
	const op = "internal/storage/devices/EnsureInitialDevice"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	var initialized bool
	if err := tx.QueryRow(ctx, `SELECT devices_initialized FROM users WHERE id=$1`, userID).Scan(&initialized); err != nil {
		return err
	}
	if initialized {
		return nil
	}
	if err := LockDeviceUser(ctx, requestID, tx, userID); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `SELECT devices_initialized FROM users WHERE id=$1`, userID).Scan(&initialized); err != nil {
		return err
	}
	if initialized {
		return nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_devices(user_id,name) VALUES($1,'Main device')`, userID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE users SET devices_initialized=true WHERE id=$1`, userID)
	return err
}
func (s Repository) CreateDevice(ctx context.Context, requestID string, userID, name string) (Device, error) {
	const op = "internal/storage/devices/CreateDevice"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	var device Device
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if err := LockDeviceUser(ctx, requestID, tx, userID); err != nil {
			return err
		}
		var limit *int
		var count int
		if err := tx.QueryRow(ctx, `SELECT device_limit,(SELECT count(*) FROM user_devices WHERE user_id=$1) FROM users WHERE id=$1`, userID).Scan(&limit, &count); err != nil {
			return err
		}
		if limit != nil && count >= *limit {
			return ErrDeviceLimit
		}
		if err := tx.QueryRow(ctx, `INSERT INTO user_devices(user_id,name) VALUES($1,$2) RETURNING id::text,user_id::text,name,created_at,updated_at`, userID, name).Scan(&device.ID, &device.UserID, &device.Name, &device.CreatedAt, &device.UpdatedAt); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET devices_initialized=true,updated_at=now() WHERE id=$1`, userID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id::text FROM subscriptions WHERE user_id=$1 ORDER BY id FOR UPDATE`, userID)
		if err != nil {
			return err
		}
		subs, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		if err := BumpNodes(ctx, requestID, tx, `SELECT sn.node_id FROM subscription_nodes sn JOIN subscriptions s ON s.id=sn.subscription_id WHERE s.user_id=$1`, userID); err != nil {
			return err
		}
		for _, sub := range subs {
			rows, err := tx.Query(ctx, `SELECT node_id::text FROM subscription_nodes WHERE subscription_id=$1 ORDER BY node_id`, sub)
			if err != nil {
				return err
			}
			nodes, err := pgx.CollectRows(rows, pgx.RowTo[string])
			if err != nil {
				return err
			}
			for _, node := range nodes {
				if err := s.InsertDevicePeer(ctx, requestID, tx, sub, node, device.ID); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return device, err
}
func (s Repository) RenameDevice(ctx context.Context, requestID string, userID, id, name string) error {
	const op = "internal/storage/devices/RenameDevice"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	tag, err := s.DB.Exec(ctx, `UPDATE user_devices SET name=$3,updated_at=now() WHERE user_id=$1 AND id=$2`, userID, id, name)
	if err == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return err
}
func (s Repository) DeleteDevice(ctx context.Context, requestID string, userID, id string) error {
	const op = "internal/storage/devices/DeleteDevice"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if err := LockDeviceUser(ctx, requestID, tx, userID); err != nil {
			return err
		}
		var owned string
		if err := tx.QueryRow(ctx, `SELECT id::text FROM user_devices WHERE id=$1 AND user_id=$2`, id, userID).Scan(&owned); err != nil {
			return err
		}
		if err := s.LockDeviceSubscriptions(ctx, requestID, tx, userID); err != nil {
			return err
		}
		if err := BumpNodes(ctx, requestID, tx, `SELECT node_id FROM peers WHERE device_id=$1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE subscriptions s SET removed_peer_last_handshake_at=greatest(s.removed_peer_last_handshake_at,h.last_seen)
   FROM (SELECT subscription_id,max(last_handshake_at) last_seen FROM peers WHERE device_id=$1 GROUP BY subscription_id) h WHERE s.id=h.subscription_id`, id); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM user_devices WHERE id=$1 AND user_id=$2`, id, userID)
		if err == nil && tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return err
	})
}
func (s Repository) LockDeviceSubscriptions(ctx context.Context, requestID string, tx pgx.Tx, userID string) error {
	const op = "internal/storage/devices/LockDeviceSubscriptions"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	if _, err := tx.Exec(ctx, `SELECT id FROM subscriptions WHERE user_id=$1 ORDER BY id FOR UPDATE`, userID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `SELECT n.id FROM nodes n WHERE EXISTS(SELECT 1 FROM subscription_nodes sn JOIN subscriptions s ON s.id=sn.subscription_id WHERE sn.node_id=n.id AND s.user_id=$1) ORDER BY n.id FOR UPDATE`, userID)
	return err
}
func (s Repository) UserDeviceProfile(ctx context.Context, requestID string, userID, deviceID, nodeID string) (vpn.ClientProfile, error) {
	const op = "internal/storage/devices/UserDeviceProfile"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return s.ClientProfile(s.DB.QueryRow(ctx, ClientProfilesQuery+`u.id=$1 AND p.device_id=$2 AND n.id=$3
 AND (s.plan_id=u.subscription_plan_id OR (u.subscription_plan_id IS NULL AND s.plan_id IS NULL)) ORDER BY s.created_at,s.id LIMIT 1`, userID, deviceID, nodeID), requestID)
}
func (s Repository) Device(ctx context.Context, requestID string, userID, deviceID string) (Device, error) {
	const op = "internal/storage/devices/Device"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	var device Device
	err := s.DB.QueryRow(ctx, SelectDevices+` WHERE d.user_id=$1 AND d.id=$2 GROUP BY d.id`, userID, deviceID).Scan(&device.ID, &device.UserID, &device.Name, &device.CreatedAt, &device.UpdatedAt, &device.LastConnectedAt, &device.Online)
	return device, err
}
func (s Repository) DeviceSubscriptionToken(ctx context.Context, requestID string, userID, deviceID, subscriptionID string) (string, error) {
	const op = "internal/storage/devices/DeviceSubscriptionToken"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	token := NewToken()
	var encrypted []byte
	err := s.DB.QueryRow(ctx, `INSERT INTO device_subscription_links(subscription_id,device_id,token_hash,token_encrypted,subscription_token_hash)
      SELECT s.id,d.id,$3,$4,s.token_hash FROM subscriptions s JOIN user_devices d ON d.user_id=s.user_id
      WHERE s.user_id=$1 AND d.id=$2 AND s.id=$5
      ON CONFLICT(subscription_id,device_id) DO UPDATE SET
      token_hash=CASE WHEN device_subscription_links.subscription_token_hash=EXCLUDED.subscription_token_hash THEN device_subscription_links.token_hash ELSE EXCLUDED.token_hash END,
      token_encrypted=CASE WHEN device_subscription_links.subscription_token_hash=EXCLUDED.subscription_token_hash THEN device_subscription_links.token_encrypted ELSE EXCLUDED.token_encrypted END,
      subscription_token_hash=EXCLUDED.subscription_token_hash
      RETURNING token_encrypted`, userID, deviceID, HashToken(token), s.Vault.Seal(token), subscriptionID).Scan(&encrypted)
	if err != nil {
		return "", err
	}
	return s.Vault.Open(encrypted)
}
func (s Repository) PublicDevices(ctx context.Context, requestID string, subscriptionID, token string) ([]Device, error) {
	const op = "internal/storage/devices/PublicDevices"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	rows, err := s.DB.Query(ctx, strings.Replace(SelectDevices, "LEFT JOIN peers p ON p.device_id=d.id", "LEFT JOIN peers p ON p.device_id=d.id AND p.subscription_id=$1", 1)+` WHERE EXISTS(SELECT 1 FROM subscriptions a WHERE a.id=$1 AND a.user_id=d.user_id
      AND (a.token_hash=$2 OR EXISTS(SELECT 1 FROM device_subscription_links l WHERE l.subscription_id=a.id AND l.subscription_token_hash=a.token_hash AND l.device_id=d.id AND l.token_hash=$2)))
      GROUP BY d.id ORDER BY d.created_at,d.id`, subscriptionID, HashToken(token))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Device])
}
func (s Repository) PublicDeviceServers(ctx context.Context, requestID string, subscriptionID, token string) ([]DeviceServer, error) {
	const op = "internal/storage/devices/PublicDeviceServers"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	rows, err := s.DB.Query(ctx, `SELECT p.device_id::text,p.node_id::text,p.last_handshake_at
 FROM peers p JOIN subscriptions s ON s.id=p.subscription_id JOIN user_devices d ON d.id=p.device_id AND d.user_id=s.user_id
 WHERE s.id=$2 AND (s.token_hash=$1 OR EXISTS(SELECT 1 FROM device_subscription_links l WHERE l.subscription_id=s.id AND l.device_id=d.id AND l.token_hash=$1 AND l.subscription_token_hash=s.token_hash))
 ORDER BY p.device_id,p.node_id`, HashToken(token), subscriptionID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[DeviceServer])
}
func (s Repository) DeviceOptions(ctx context.Context, requestID string, userID string) ([]DeviceOption, error) {
	const op = "internal/storage/devices/DeviceOptions"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	rows, err := s.DB.Query(ctx, `SELECT id::text,name FROM user_devices WHERE user_id=$1 ORDER BY created_at,id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[DeviceOption])
}
