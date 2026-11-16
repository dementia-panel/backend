package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"dementia/backend/internal/vpn"
	"github.com/jackc/pgx/v5"
)

var ErrDeviceLimit = errors.New("device limit reached")
var ErrAmbiguousUser = errors.New("telegram_id belongs to multiple users; use UUID")

type Device struct {
	ID              string     `json:"id"`
	UserID          string     `json:"user_id"`
	Name            string     `json:"name"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	LastConnectedAt *time.Time `json:"last_connected_at"`
	Online          bool       `json:"online"`
}

func (s Store) UserIDByTelegram(ctx context.Context, telegramID int64) (string, error) {
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

func (s Store) DefaultDeviceLimit(ctx context.Context) (*int, error) {
	var limit *int
	err := s.DB.QueryRow(ctx, `SELECT default_device_limit FROM device_settings WHERE id`).Scan(&limit)
	return limit, err
}
func (s Store) SetDefaultDeviceLimit(ctx context.Context, limit *int) error {
	_, err := s.DB.Exec(ctx, `UPDATE device_settings SET default_device_limit=$1 WHERE id`, limit)
	return err
}
func (s Store) SetUserDeviceLimit(ctx context.Context, userID string, limit *int) error {
	tag, err := s.DB.Exec(ctx, `UPDATE users SET device_limit=$2,updated_at=now() WHERE id=$1`, userID, limit)
	if err == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return err
}

const selectDevices = `SELECT d.id::text,d.user_id::text,d.name,d.created_at,d.updated_at,max(p.last_handshake_at),
 coalesce(bool_or(u.enabled AND s.enabled AND (s.expires_at IS NULL OR s.expires_at>now())
 AND (s.traffic_limit_bytes IS NULL OR s.traffic_used_bytes<s.traffic_limit_bytes)
 AND n.enabled AND n.status='ready' AND n.retired_at IS NULL AND n.applied_revision=n.revision
 AND n.handshakes_reported_at>now()-interval '1 minute' AND p.current_handshake_at>now()-interval '3 minutes'),false)
 FROM user_devices d JOIN users u ON u.id=d.user_id LEFT JOIN peers p ON p.device_id=d.id
 LEFT JOIN subscriptions s ON s.id=p.subscription_id LEFT JOIN nodes n ON n.id=p.node_id`

func (s Store) Devices(ctx context.Context, userID string) ([]Device, error) {
	rows, err := s.DB.Query(ctx, selectDevices+` WHERE d.user_id=$1 GROUP BY d.id ORDER BY d.created_at,d.id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.AppendRows([]Device{}, rows, pgx.RowToStructByPos[Device])
}

func lockDeviceUser(ctx context.Context, tx pgx.Tx, userID string) error {
	var id string
	return tx.QueryRow(ctx, `SELECT id::text FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&id)
}
func (s Store) ensureInitialDevice(ctx context.Context, tx pgx.Tx, userID string) error {
	var initialized bool
	if err := tx.QueryRow(ctx, `SELECT devices_initialized FROM users WHERE id=$1`, userID).Scan(&initialized); err != nil {
		return err
	}
	if initialized {
		return nil
	}
	if err := lockDeviceUser(ctx, tx, userID); err != nil {
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
func (s Store) CreateDevice(ctx context.Context, userID, name string) (Device, error) {
	var device Device
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if err := lockDeviceUser(ctx, tx, userID); err != nil {
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
		if err := bumpNodes(ctx, tx, `SELECT sn.node_id FROM subscription_nodes sn JOIN subscriptions s ON s.id=sn.subscription_id WHERE s.user_id=$1`, userID); err != nil {
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
				if err := s.insertDevicePeer(ctx, tx, sub, node, device.ID); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return device, err
}
func (s Store) RenameDevice(ctx context.Context, userID, id, name string) error {
	tag, err := s.DB.Exec(ctx, `UPDATE user_devices SET name=$3,updated_at=now() WHERE user_id=$1 AND id=$2`, userID, id, name)
	if err == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return err
}
func (s Store) DeleteDevice(ctx context.Context, userID, id string) error {
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if err := lockDeviceUser(ctx, tx, userID); err != nil {
			return err
		}
		var owned string
		if err := tx.QueryRow(ctx, `SELECT id::text FROM user_devices WHERE id=$1 AND user_id=$2`, id, userID).Scan(&owned); err != nil {
			return err
		}
		if err := s.lockDeviceSubscriptions(ctx, tx, userID); err != nil {
			return err
		}
		if err := bumpNodes(ctx, tx, `SELECT node_id FROM peers WHERE device_id=$1`, id); err != nil {
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

func (s Store) lockDeviceSubscriptions(ctx context.Context, tx pgx.Tx, userID string) error {
	if _, err := tx.Exec(ctx, `SELECT id FROM subscriptions WHERE user_id=$1 ORDER BY id FOR UPDATE`, userID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `SELECT n.id FROM nodes n WHERE EXISTS(SELECT 1 FROM subscription_nodes sn JOIN subscriptions s ON s.id=sn.subscription_id WHERE sn.node_id=n.id AND s.user_id=$1) ORDER BY n.id FOR UPDATE`, userID)
	return err
}

func (s Store) UserDeviceProfile(ctx context.Context, userID, deviceID, nodeID string) (vpn.ClientProfile, error) {
	return s.clientProfile(s.DB.QueryRow(ctx, clientProfilesQuery+`u.id=$1 AND p.device_id=$2 AND n.id=$3
 AND (s.plan_id=u.subscription_plan_id OR (u.subscription_plan_id IS NULL AND s.plan_id IS NULL)) ORDER BY s.created_at,s.id LIMIT 1`, userID, deviceID, nodeID))
}

func (s Store) Device(ctx context.Context, userID, deviceID string) (Device, error) {
	var device Device
	err := s.DB.QueryRow(ctx, selectDevices+` WHERE d.user_id=$1 AND d.id=$2 GROUP BY d.id`, userID, deviceID).Scan(
		&device.ID, &device.UserID, &device.Name, &device.CreatedAt, &device.UpdatedAt, &device.LastConnectedAt, &device.Online)
	return device, err
}

// DeviceSubscriptionToken returns the existing bearer token without rotating VPN keys.
func (s Store) DeviceSubscriptionToken(ctx context.Context, userID, deviceID, subscriptionID string) (string, error) {
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

func (s Store) PublicDevices(ctx context.Context, subscriptionID, token string) ([]Device, error) {
	rows, err := s.DB.Query(ctx, strings.Replace(selectDevices, "LEFT JOIN peers p ON p.device_id=d.id", "LEFT JOIN peers p ON p.device_id=d.id AND p.subscription_id=$1", 1)+` WHERE EXISTS(SELECT 1 FROM subscriptions a WHERE a.id=$1 AND a.user_id=d.user_id
      AND (a.token_hash=$2 OR EXISTS(SELECT 1 FROM device_subscription_links l WHERE l.subscription_id=a.id AND l.subscription_token_hash=a.token_hash AND l.device_id=d.id AND l.token_hash=$2)))
      GROUP BY d.id ORDER BY d.created_at,d.id`, subscriptionID, HashToken(token))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Device])
}
