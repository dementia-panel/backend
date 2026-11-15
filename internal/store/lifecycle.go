package store

import (
	"context"
	"time"

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
		tag, err := tx.Exec(ctx, "UPDATE subscriptions SET token_hash=$2 WHERE id=$1", id, HashToken(token))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		if err := bumpNodes(ctx, tx, "SELECT node_id FROM peers WHERE subscription_id=$1", id); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "SELECT node_id::text FROM peers WHERE subscription_id=$1", id)
		if err != nil {
			return err
		}
		nodes, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, nodeID := range nodes {
			private, public, err := vpn.KeyPair()
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE peers SET private_key=$3,public_key=$4,current_handshake_at=NULL,received_counter=0,sent_counter=0
				WHERE subscription_id=$1 AND node_id=$2`, id, nodeID, s.Vault.Seal(private), public); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `INSERT INTO events(scope,event,data)
			SELECT 'subscription','subscription.revoked',jsonb_build_object('id',id,'user_id',user_id)
			FROM subscriptions WHERE id=$1 AND current_setting('dementia.webhook_enabled',true)='on'`, id)
		return err
	})
}

func (s Store) RetireNode(ctx context.Context, id string) (Node, error) {
	var node Node
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		var previous *time.Time
		if err := tx.QueryRow(ctx, "SELECT retired_at FROM nodes WHERE id=$1 FOR UPDATE", id).Scan(&previous); err != nil {
			return err
		}
		var err error
		node, err = scanNode(tx.QueryRow(ctx, `UPDATE nodes SET retired_at=coalesce(retired_at,now()),enabled=false,
			revision=revision+CASE WHEN retired_at IS NULL THEN 1 ELSE 0 END
			WHERE id=$1 RETURNING `+nodeColumns, id))
		if err != nil || previous != nil {
			return err
		}
		return recordDeletion(ctx, tx, "node", id, node.Name)
	})
	return node, err
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
