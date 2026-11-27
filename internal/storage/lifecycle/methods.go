package lifecycle

import (
	"context"
	"dementia/backend/internal/lib/logger/with"
	. "dementia/backend/internal/storage/shared"
	"dementia/backend/internal/vpn"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

func (s Repository) DeleteUser(ctx context.Context, requestID string, id string) error {
	const op = "internal/storage/lifecycle/DeleteUser"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		var username string
		if err := tx.QueryRow(ctx, "SELECT username FROM users WHERE id=$1 FOR UPDATE", id).Scan(&username); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SELECT id FROM subscriptions WHERE user_id=$1 ORDER BY id FOR UPDATE", id); err != nil {
			return err
		}
		if err := BumpNodes(ctx, requestID, tx, "SELECT p.node_id FROM peers p JOIN subscriptions s ON s.id=p.subscription_id WHERE s.user_id=$1", id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM peers USING subscriptions s WHERE peers.subscription_id=s.id AND s.user_id=$1", id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM subscriptions WHERE user_id=$1", id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM users WHERE id=$1", id); err != nil {
			return err
		}
		return RecordDeletion(ctx, requestID, tx, "user", id, username)
	})
}
func (s Repository) DeleteSubscription(ctx context.Context, requestID string, id string) error {
	const op = "internal/storage/lifecycle/DeleteSubscription"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		var name string
		if err := tx.QueryRow(ctx, "SELECT name FROM subscriptions WHERE id=$1 FOR UPDATE", id).Scan(&name); err != nil {
			return err
		}
		if err := BumpNodes(ctx, requestID, tx, "SELECT node_id FROM peers WHERE subscription_id=$1", id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM peers WHERE subscription_id=$1", id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM subscriptions WHERE id=$1", id); err != nil {
			return err
		}
		return RecordDeletion(ctx, requestID, tx, "subscription", id, name)
	})
}
func (s Repository) RevokeSubscription(ctx context.Context, requestID string, id, token string) error {
	const op = "internal/storage/lifecycle/RevokeSubscription"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "UPDATE subscriptions SET token_hash=$2,token_encrypted=$3 WHERE id=$1", id, HashToken(token), s.Vault.Seal(token))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		if err := BumpNodes(ctx, requestID, tx, "SELECT node_id FROM peers WHERE subscription_id=$1", id); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "SELECT public_key FROM peers WHERE subscription_id=$1 ORDER BY node_id,device_id", id)
		if err != nil {
			return err
		}
		nodes, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, oldPublic := range nodes {
			private, public, err := vpn.KeyPair()
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE peers SET private_key=$3,public_key=$4,current_handshake_at=NULL,received_counter=0,sent_counter=0
				WHERE subscription_id=$1 AND public_key=$2`, id, oldPublic, s.Vault.Seal(private), public); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `INSERT INTO events(scope,event,data)
			SELECT 'subscription','subscription.revoked',jsonb_build_object('id',id,'user_id',user_id)
			FROM subscriptions WHERE id=$1 AND current_setting('dementia.webhook_enabled',true)='on'`, id)
		return err
	})
}
func (s Repository) InstallStage(ctx context.Context, requestID string, id, token, stage string) error {
	const op = "internal/storage/lifecycle/InstallStage"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	_, err := s.DB.Exec(ctx, `UPDATE nodes SET operation_stage=$3 WHERE id=$1 AND agent_token_hash=$2 AND status='installing'`, id, HashToken(token), stage)
	return err
}
func (s Repository) BeginNodeRemoval(ctx context.Context, requestID string, id string) (Node, error) {
	const op = "internal/storage/lifecycle/BeginNodeRemoval"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return ScanNode(s.DB.QueryRow(ctx, `UPDATE nodes SET operation_stage='removing',error=NULL
		WHERE id=$1 AND retired_at IS NULL AND status<>'installing' AND operation_stage NOT IN ('removing','upgrading') RETURNING `+NodeColumns, id))
}
func (s Repository) VerifyRemovalHostKey(ctx context.Context, requestID string, id, key string) error {
	const op = "internal/storage/lifecycle/VerifyRemovalHostKey"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	var ok bool
	err := s.DB.QueryRow(ctx, `SELECT host_key='' OR host_key=$2 FROM nodes WHERE id=$1 AND operation_stage='removing'`, id, key).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNodeUnavailable
	}
	return nil
}
func (s Repository) FinishNodeRemoval(ctx context.Context, requestID string, id, failure string) error {
	const op = "internal/storage/lifecycle/FinishNodeRemoval"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	if failure != "" {
		_, err := s.DB.Exec(ctx, `UPDATE nodes SET operation_stage='remove_failed',error=$2 WHERE id=$1 AND operation_stage='removing'`, id, failure)
		return err
	}
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		return RemoveNodeData(ctx, requestID, tx, id, false)
	})
}
func (s Repository) ForgetNode(ctx context.Context, requestID string, id string) error {
	const op = "internal/storage/lifecycle/ForgetNode"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		return RemoveNodeData(ctx, requestID, tx, id, true)
	})
}
func (s Repository) RestartAllNodes(ctx context.Context, requestID string) (int64, error) {
	const op = "internal/storage/lifecycle/RestartAllNodes"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	tag, err := s.DB.Exec(ctx, `WITH ready AS (
		SELECT id FROM nodes WHERE status='ready' AND retired_at IS NULL ORDER BY id FOR UPDATE
	) UPDATE nodes n SET restart_revision=restart_revision+1 FROM ready WHERE ready.id=n.id`)
	return tag.RowsAffected(), err
}
func (s Repository) BulkUserEnabled(ctx context.Context, requestID string, ids []string, enabled bool) (int64, error) {
	const op = "internal/storage/lifecycle/BulkUserEnabled"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	var updated int64
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "SELECT id::text FROM users WHERE id=ANY($1::text[]::uuid[]) ORDER BY id FOR UPDATE", ids)
		if err != nil {
			return err
		}
		found, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		if len(found) != len(ids) {
			return pgx.ErrNoRows
		}
		tag, err := tx.Exec(ctx, `WITH changed AS (
			UPDATE users SET enabled=$2,updated_at=now()
			WHERE id=ANY($1::text[]::uuid[]) AND enabled<>$2 RETURNING id
		), affected AS (
			SELECT n.id FROM nodes n WHERE n.id IN (
				SELECT p.node_id FROM peers p JOIN subscriptions s ON s.id=p.subscription_id JOIN changed c ON c.id=s.user_id
			) ORDER BY n.id FOR UPDATE
		), bumped AS (
			UPDATE nodes n SET revision=n.revision+1 FROM affected a WHERE n.id=a.id
		) SELECT id FROM changed`, ids, enabled)
		updated = tag.RowsAffected()
		return err
	})
	return updated, err
}
func (s Repository) SetNodeEndpoint(ctx context.Context, requestID string, id string, host *string, port *int) (Node, error) {
	const op = "internal/storage/lifecycle/SetNodeEndpoint"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return ScanNode(s.DB.QueryRow(ctx, `UPDATE nodes SET endpoint_host=$2,endpoint_port=$3
		WHERE id=$1 AND retired_at IS NULL RETURNING `+NodeColumns, id, host, port))
}
