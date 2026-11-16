package store

import (
	"context"

	"dementia/backend/internal/vpn"
	"github.com/jackc/pgx/v5"
)

func (s Store) DeleteUser(ctx context.Context, id string) error {
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		var username string
		if err := tx.QueryRow(ctx, "SELECT username FROM users WHERE id=$1 FOR UPDATE", id).Scan(&username); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SELECT id FROM subscriptions WHERE user_id=$1 ORDER BY id FOR UPDATE", id); err != nil {
			return err
		}
		if err := bumpNodes(ctx, tx, "SELECT p.node_id FROM peers p JOIN subscriptions s ON s.id=p.subscription_id WHERE s.user_id=$1", id); err != nil {
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
		return recordDeletion(ctx, tx, "user", id, username)
	})
}

func (s Store) DeleteSubscription(ctx context.Context, id string) error {
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		var name string
		if err := tx.QueryRow(ctx, "SELECT name FROM subscriptions WHERE id=$1 FOR UPDATE", id).Scan(&name); err != nil {
			return err
		}
		if err := bumpNodes(ctx, tx, "SELECT node_id FROM peers WHERE subscription_id=$1", id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM peers WHERE subscription_id=$1", id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM subscriptions WHERE id=$1", id); err != nil {
			return err
		}
		return recordDeletion(ctx, tx, "subscription", id, name)
	})
}

func (s Store) RevokeSubscription(ctx context.Context, id, token string) error {
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "UPDATE subscriptions SET token_hash=$2,token_encrypted=$3 WHERE id=$1", id, HashToken(token), s.Vault.Seal(token))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		if err := bumpNodes(ctx, tx, "SELECT node_id FROM peers WHERE subscription_id=$1", id); err != nil {
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

func (s Store) InstallStage(ctx context.Context, id, token, stage string) error {
	_, err := s.DB.Exec(ctx, `UPDATE nodes SET operation_stage=$3 WHERE id=$1 AND agent_token_hash=$2 AND status='installing'`, id, HashToken(token), stage)
	return err
}

func (s Store) BeginNodeRemoval(ctx context.Context, id string) (Node, error) {
	return scanNode(s.DB.QueryRow(ctx, `UPDATE nodes SET operation_stage='removing',error=NULL
		WHERE id=$1 AND retired_at IS NULL AND status<>'installing' AND operation_stage<>'removing' RETURNING `+nodeColumns, id))
}

func (s Store) VerifyRemovalHostKey(ctx context.Context, id, key string) error {
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

func (s Store) FinishNodeRemoval(ctx context.Context, id, failure string) error {
	if failure != "" {
		_, err := s.DB.Exec(ctx, `UPDATE nodes SET operation_stage='remove_failed',error=$2 WHERE id=$1 AND operation_stage='removing'`, id, failure)
		return err
	}
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT s.id FROM subscriptions s JOIN peers p ON p.subscription_id=s.id
			WHERE p.node_id=$1 ORDER BY s.id FOR UPDATE OF s`, id); err != nil {
			return err
		}
		var name string
		if err := tx.QueryRow(ctx, `UPDATE nodes SET retired_at=now(),enabled=false,operation_stage='removed',revision=revision+1
			WHERE id=$1 AND operation_stage='removing' RETURNING name`, id).Scan(&name); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `WITH removed AS (DELETE FROM peers WHERE node_id=$1 RETURNING subscription_id,last_handshake_at)
				UPDATE subscriptions s SET removed_peer_last_handshake_at=greatest(s.removed_peer_last_handshake_at,r.last_handshake_at)
				FROM (SELECT subscription_id,max(last_handshake_at) AS last_handshake_at FROM removed GROUP BY subscription_id) r WHERE s.id=r.subscription_id`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM subscription_plan_nodes WHERE node_id=$1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM subscription_nodes WHERE node_id=$1`, id); err != nil {
			return err
		}
		return recordDeletion(ctx, tx, "node", id, name)
	})
}

func (s Store) RestartAllNodes(ctx context.Context) (int64, error) {
	tag, err := s.DB.Exec(ctx, `WITH ready AS (
		SELECT id FROM nodes WHERE status='ready' AND retired_at IS NULL ORDER BY id FOR UPDATE
	) UPDATE nodes n SET restart_revision=restart_revision+1 FROM ready WHERE ready.id=n.id`)
	return tag.RowsAffected(), err
}

func recordDeletion(ctx context.Context, tx pgx.Tx, scope, id, name string) error {
	_, err := tx.Exec(ctx, `INSERT INTO events(scope,event,data)
		SELECT $1::text,$1::text||'.deleted',jsonb_build_object('id',$2::text,'name',$3::text)
		WHERE current_setting('dementia.webhook_enabled',true)='on'
		OR ($1='node' AND current_setting('dementia.telegram_enabled',true)='on')`, scope, id, name)
	return err
}

func (s Store) BulkUserEnabled(ctx context.Context, ids []string, enabled bool) (int64, error) {
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

func (s Store) SetNodeEndpoint(ctx context.Context, id string, host *string, port *int) (Node, error) {
	return scanNode(s.DB.QueryRow(ctx, `UPDATE nodes SET endpoint_host=$2,endpoint_port=$3
		WHERE id=$1 AND retired_at IS NULL RETURNING `+nodeColumns, id, host, port))
}
