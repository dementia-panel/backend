package subscriptions

import (
	"bytes"
	"context"
	"dementia/backend/internal/lib/logger/with"
	. "dementia/backend/internal/storage/shared"
	"dementia/backend/internal/vpn"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s Repository) CreateSubscription(ctx context.Context, requestID string, userID, name, token string, expires time.Time, nodeIDs []string) (Subscription, error) {
	const op = "internal/storage/subscriptions/CreateSubscription"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	var sub Subscription
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		var user string
		if err := tx.QueryRow(ctx, `SELECT id::text FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&user); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO subscriptions(user_id,name,token_hash,expires_at,token_encrypted)
			VALUES($1,$2,$3,$4,$5) RETURNING id::text,user_id::text,name,enabled,expires_at`, userID, name, HashToken(token), expires, s.Vault.Seal(token)).Scan(&sub.ID, &sub.UserID, &sub.Name, &sub.Enabled, &sub.ExpiresAt); err != nil {
			return err
		}
		if err := s.EnsureInitialDevice(ctx, requestID, tx, userID); err != nil {
			return err
		}
		for _, nodeID := range nodeIDs {
			tag, err := tx.Exec(ctx, `UPDATE nodes SET revision=revision+1 WHERE id=$1 AND status='ready' AND enabled`, nodeID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return fmt.Errorf("%w: %s", ErrNodeUnavailable, nodeID)
			}
			if err := s.InsertPeer(ctx, requestID, tx, sub.ID, nodeID); err != nil {
				return err
			}
		}
		return nil
	})
	sub.NodeIDs = nodeIDs
	return sub, err
}
func (s Repository) InsertPeer(ctx context.Context, requestID string, tx pgx.Tx, subscriptionID, nodeID string) error {
	const op = "internal/storage/subscriptions/InsertPeer"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	var userID string
	if err := tx.QueryRow(ctx, `SELECT user_id::text FROM subscriptions WHERE id=$1`, subscriptionID).Scan(&userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO subscription_nodes(subscription_id,node_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, subscriptionID, nodeID); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id::text FROM user_devices WHERE user_id=$1 ORDER BY created_at,id`, userID)
	if err != nil {
		return err
	}
	devices, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, id := range devices {
		if err := s.InsertDevicePeer(ctx, requestID, tx, subscriptionID, nodeID, id); err != nil {
			return err
		}
	}
	return nil
}
func (s Repository) InsertDevicePeer(ctx context.Context, requestID string, tx pgx.Tx, subscriptionID, nodeID, deviceID string) error {
	const op = "internal/storage/subscriptions/InsertDevicePeer"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	var address int
	err := tx.QueryRow(ctx, `SELECT min(candidate) FROM (
			SELECT 2 AS candidate
			UNION ALL SELECT address+1 FROM peers WHERE node_id=$1 AND address<65534
		) candidates
		WHERE NOT EXISTS (SELECT 1 FROM peers WHERE node_id=$1 AND address=candidate)
		HAVING count(*)>0`, nodeID).Scan(&address)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrAddressPoolFull, nodeID)
	}
	if err != nil {
		return err
	}
	private, public, err := vpn.KeyPair()
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO peers(subscription_id,node_id,address,private_key,public_key,device_id)
		VALUES($1,$2,$3,$4,$5,$6)`, subscriptionID, nodeID, address, s.Vault.Seal(private), public, deviceID); err != nil {
		return err
	}
	return nil
}
func (s Repository) ReplaceSubscriptionNodes(ctx context.Context, requestID string, subscriptionID string, nodeIDs []string) error {
	const op = "internal/storage/subscriptions/ReplaceSubscriptionNodes"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		return s.ReplaceSubscriptionNodesTx(ctx, requestID, tx, subscriptionID, nodeIDs)
	})
}
func (s Repository) ReplaceSubscriptionNodesTx(ctx context.Context, requestID string, tx pgx.Tx, subscriptionID string, nodeIDs []string) error {
	const op = "internal/storage/subscriptions/ReplaceSubscriptionNodesTx"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	var id string
	if err := tx.QueryRow(ctx, "SELECT id::text FROM subscriptions WHERE id=$1 FOR UPDATE", subscriptionID).Scan(&id); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT n.id::text,n.enabled AND n.status='ready',p.node_id IS NOT NULL
		FROM nodes n LEFT JOIN subscription_nodes p ON p.node_id=n.id AND p.subscription_id=$1
		WHERE n.id=ANY($2::text[]::uuid[]) OR p.node_id IS NOT NULL ORDER BY n.id FOR UPDATE OF n`, id, nodeIDs)
	if err != nil {
		return err
	}
	type assignment struct {
		id              string
		ready, assigned bool
	}
	assignments, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (assignment, error) {
		var item assignment
		err := row.Scan(&item.id, &item.ready, &item.assigned)
		return item, err
	})
	if err != nil {
		return err
	}
	changed := false
	wanted := make(map[string]bool, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		wanted[nodeID] = true
	}
	for _, item := range assignments {
		keep := wanted[item.id]
		delete(wanted, item.id)
		if keep == item.assigned {
			continue
		}
		changed = true
		if keep && !item.ready {
			return fmt.Errorf("%w: %s", ErrNodeUnavailable, item.id)
		}
		if _, err := tx.Exec(ctx, "UPDATE nodes SET revision=revision+1 WHERE id=$1", item.id); err != nil {
			return err
		}
		if keep {
			if err := s.InsertPeer(ctx, requestID, tx, id, item.id); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `WITH removed AS (
			DELETE FROM peers WHERE subscription_id=$1 AND node_id=$2 RETURNING last_handshake_at
		) UPDATE subscriptions SET removed_peer_last_handshake_at=greatest(
			removed_peer_last_handshake_at,(SELECT max(last_handshake_at) FROM removed)) WHERE id=$1`, id, item.id); err != nil {
			return err
		}
		if !keep {
			if _, err := tx.Exec(ctx, `DELETE FROM subscription_nodes WHERE subscription_id=$1 AND node_id=$2`, id, item.id); err != nil {
				return err
			}
		}
	}
	if len(wanted) != 0 {
		return ErrNodeUnavailable
	}
	if changed {
		_, err := tx.Exec(ctx, `INSERT INTO events(scope,event,data)
			SELECT 'subscription','subscription.nodes_changed',jsonb_build_object('id',id,'user_id',user_id,'node_ids',$2::text[])
			FROM subscriptions WHERE id=$1 AND current_setting('dementia.webhook_enabled', true)='on'`, id, nodeIDs)
		return err
	}
	return nil
}
func (s Repository) Subscriptions(ctx context.Context, requestID string) ([]Subscription, error) {
	const op = "internal/storage/subscriptions/Subscriptions"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	rows, err := s.DB.Query(ctx, `SELECT s.id::text,s.user_id::text,s.name,s.enabled,s.expires_at,s.traffic_limit_bytes,s.traffic_used_bytes,s.traffic_reset_days,s.traffic_reset_at,s.plan_id::text,
		ARRAY(SELECT p.node_id::text FROM subscription_nodes p WHERE p.subscription_id=s.id ORDER BY p.node_id)
		FROM subscriptions s ORDER BY s.created_at,s.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Subscription{}
	for rows.Next() {
		var sub Subscription
		if err := rows.Scan(&sub.ID, &sub.UserID, &sub.Name, &sub.Enabled, &sub.ExpiresAt, &sub.TrafficLimit, &sub.TrafficUsed, &sub.TrafficResetDays, &sub.TrafficResetAt, &sub.PlanID, &sub.NodeIDs); err != nil {
			return nil, err
		}
		result = append(result, sub)
	}
	return result, rows.Err()
}
func (s Repository) UpdateSubscription(ctx context.Context, requestID string, id string, enabled *bool, expiry *time.Time) error {
	const op = "internal/storage/subscriptions/UpdateSubscription"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE subscriptions SET enabled=coalesce($2,enabled),expires_at=coalesce($3,expires_at) WHERE id=$1`, id, enabled, expiry)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return BumpNodes(ctx, requestID, tx, `SELECT node_id FROM peers WHERE subscription_id=$1`, id)
	})
}
func (s Repository) RotateToken(ctx context.Context, requestID string, id, token string) error {
	const op = "internal/storage/subscriptions/RotateToken"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	tag, err := s.DB.Exec(ctx, `UPDATE subscriptions SET token_hash=$2,token_encrypted=$3 WHERE id=$1`, id, HashToken(token), s.Vault.Seal(token))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}
func (s Repository) PublicSubscription(ctx context.Context, requestID string, token string) (Subscription, []Location, error) {
	const op = "internal/storage/subscriptions/PublicSubscription"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	var sub Subscription
	locations := []Location{}
	err := pgx.BeginTxFunc(ctx, s.DB, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT s.id::text,s.name,s.enabled,s.expires_at,s.user_id::text
			FROM subscriptions s JOIN users u ON u.id=s.user_id
			WHERE (s.token_hash=$1 OR EXISTS(SELECT 1 FROM device_subscription_links l JOIN user_devices d ON d.id=l.device_id AND d.user_id=s.user_id WHERE l.subscription_id=s.id AND l.subscription_token_hash=s.token_hash AND l.token_hash=$1)) AND s.enabled AND u.enabled AND (s.expires_at IS NULL OR s.expires_at>now()) AND (s.traffic_limit_bytes IS NULL OR s.traffic_used_bytes<s.traffic_limit_bytes)`, HashToken(token)).Scan(&sub.ID, &sub.Name, &sub.Enabled, &sub.ExpiresAt, &sub.UserID)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT n.id::text,n.name,coalesce(n.enabled AND n.status='ready' AND n.applied_revision=n.revision AND n.last_seen_at>now()-interval '1 minute',false)
			FROM subscription_nodes p JOIN nodes n ON n.id=p.node_id WHERE p.subscription_id=$1 ORDER BY n.name,n.id`, sub.ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var l Location
			if err := rows.Scan(&l.ID, &l.Name, &l.Available); err != nil {
				return err
			}
			locations = append(locations, l)
		}
		return rows.Err()
	})
	return sub, locations, err
}
func (s Repository) Profile(ctx context.Context, requestID string, token, nodeID string, deviceIDs ...string) (vpn.ClientProfile, error) {
	const op = "internal/storage/subscriptions/Profile"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return s.ClientProfile(s.DB.QueryRow(ctx, ClientProfilesQuery+`n.id=$1 AND p.device_id=(SELECT d.id FROM user_devices d WHERE d.user_id=u.id
        AND (s.token_hash=$2 OR EXISTS(SELECT 1 FROM device_subscription_links l WHERE l.subscription_id=s.id AND l.subscription_token_hash=s.token_hash AND l.device_id=d.id AND l.token_hash=$2)) AND (nullif($3,'')::uuid IS NULL OR d.id=nullif($3,'')::uuid) ORDER BY d.created_at,d.id LIMIT 1) AND `+SyncedProfile, nodeID, HashToken(token), FirstDeviceID(deviceIDs)), requestID)
}
func (s Repository) ProfileForSubscription(ctx context.Context, requestID string, subscriptionID, nodeID string, deviceIDs ...string) (vpn.ClientProfile, error) {
	const op = "internal/storage/subscriptions/ProfileForSubscription"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return s.ClientProfile(s.DB.QueryRow(ctx, ClientProfilesQuery+`n.id=$1 AND s.id=$2 AND p.device_id=(SELECT d.id FROM user_devices d WHERE d.user_id=u.id AND (nullif($3,'')::uuid IS NULL OR d.id=nullif($3,'')::uuid) ORDER BY d.created_at,d.id LIMIT 1)`, nodeID, subscriptionID, FirstDeviceID(deviceIDs)), requestID)
}
func (s Repository) PublicProfiles(ctx context.Context, requestID string, token string) ([]vpn.ClientProfile, error) {
	const op = "internal/storage/subscriptions/PublicProfiles"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	rows, err := s.DB.Query(ctx, ClientProfilesQuery+`p.device_id=(SELECT d.id FROM user_devices d WHERE d.user_id=u.id AND
        (s.token_hash=$1 OR EXISTS(SELECT 1 FROM device_subscription_links l WHERE l.subscription_id=s.id AND l.subscription_token_hash=s.token_hash AND l.device_id=d.id AND l.token_hash=$1)) ORDER BY d.created_at,d.id LIMIT 1) AND `+SyncedProfile+` ORDER BY n.name,n.id`, HashToken(token))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var profiles []vpn.ClientProfile
	for rows.Next() {
		profile, err := s.ClientProfile(rows, requestID)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, rows.Err()
}
func (s Repository) ClientProfile(row pgx.Row, requestID string) (vpn.ClientProfile, error) {
	const op = "internal/storage/subscriptions/ClientProfile"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	var name, ip, public string
	var private, header []byte
	var address, port int
	err := row.Scan(&name, &ip, &port, &public, &header, &private, &address)
	if err != nil {
		return vpn.ClientProfile{}, err
	}
	privateKey, err := s.Vault.Open(private)
	if err != nil {
		return vpn.ClientProfile{}, err
	}
	headerKey, err := s.Vault.Open(header)
	if err != nil {
		return vpn.ClientProfile{}, err
	}
	return vpn.ClientProfile{Name: name, PrivateKey: privateKey, PublicKey: public, HeaderKey: headerKey, IP: ip, Port: port, Address: vpn.Address(address)}, nil
}
func (s Repository) SubscriptionToken(ctx context.Context, requestID string, id string) (string, error) {
	const op = "internal/storage/subscriptions/SubscriptionToken"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	var encrypted, hash []byte
	if err := s.DB.QueryRow(ctx, `SELECT token_encrypted,token_hash FROM subscriptions WHERE id=$1`, id).Scan(&encrypted, &hash); err != nil {
		return "", err
	}
	if len(encrypted) == 0 {
		return "", nil
	}
	token, err := s.Vault.Open(encrypted)
	if err != nil {
		return "", err
	}
	if !bytes.Equal(HashToken(token), hash) {
		return "", errors.New("subscription token integrity check failed")
	}
	return token, nil
}
