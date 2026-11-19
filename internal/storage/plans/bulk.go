package plans

import (
	"context"
	. "dementia/backend/internal/storage/shared"
	"dementia/backend/internal/vpn"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
)

type planNode struct {
	id      string
	ready   bool
	retired bool
}

type planPeer struct {
	subscriptionID string
	deviceID       string
}

func (s Repository) replacePlanNodes(ctx context.Context, tx pgx.Tx, planID string, accessIDs, nodeIDs []string) error {
	rows, err := tx.Query(ctx, `SELECT id::text,enabled AND status='ready',retired_at IS NOT NULL FROM nodes
		WHERE id=ANY($1::text[]::uuid[]) OR id IN
		(SELECT node_id FROM subscription_nodes WHERE subscription_id=ANY($2::text[]::uuid[]))
		ORDER BY id FOR UPDATE`, nodeIDs, accessIDs)
	if err != nil {
		return err
	}
	nodes, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (planNode, error) {
		var node planNode
		err := row.Scan(&node.id, &node.ready, &node.retired)
		return node, err
	})
	if err != nil {
		return err
	}
	wanted := make(map[string]bool, len(nodeIDs))
	for _, id := range nodeIDs {
		wanted[id] = true
	}
	if len(wanted) != len(nodeIDs) {
		return ErrNodeUnavailable
	}
	for _, node := range nodes {
		if wanted[node.id] {
			if node.retired {
				return ErrNodeUnavailable
			}
			delete(wanted, node.id)
		}
	}
	if len(wanted) != 0 {
		return ErrNodeUnavailable
	}
	if _, err := tx.Exec(ctx, `DELETE FROM subscription_plan_nodes WHERE plan_id=$1 AND node_id<>ALL($2::text[]::uuid[])`, planID, nodeIDs); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO subscription_plan_nodes(plan_id,node_id)
		SELECT $1,unnest($2::text[]::uuid[]) ON CONFLICT DO NOTHING`, planID, nodeIDs); err != nil {
		return err
	}
	changed := make(map[string]bool)
	for _, node := range nodes {
		if containsNode(nodeIDs, node.id) {
			members, err := s.addPlanNode(ctx, tx, planID, node)
			if err != nil {
				return err
			}
			for _, id := range members {
				changed[id] = true
			}
			if len(members) == 0 {
				continue
			}
		} else {
			members, err := s.removePlanNode(ctx, tx, planID, node.id)
			if err != nil {
				return err
			}
			for _, id := range members {
				changed[id] = true
			}
			if len(members) == 0 {
				continue
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE nodes SET revision=revision+1 WHERE id=$1`, node.id); err != nil {
			return err
		}
	}
	if len(changed) == 0 {
		return nil
	}
	ids := make([]string, 0, len(changed))
	for id := range changed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	_, err = tx.Exec(ctx, `INSERT INTO events(scope,event,data)
		SELECT 'subscription','subscription.nodes_changed',jsonb_build_object('id',id,'user_id',user_id,'node_ids',$2::text[])
		FROM subscriptions WHERE id=ANY($1::text[]::uuid[]) AND current_setting('dementia.webhook_enabled',true)='on'`, ids, nodeIDs)
	return err
}

func containsNode(ids []string, id string) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}

func (s Repository) addPlanNode(ctx context.Context, tx pgx.Tx, planID string, node planNode) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT s.id::text FROM subscriptions s
		LEFT JOIN subscription_nodes sn ON sn.subscription_id=s.id AND sn.node_id=$2
		WHERE s.plan_id=$1 AND sn.subscription_id IS NULL ORDER BY s.id`, planID, node.id)
	if err != nil {
		return nil, err
	}
	members, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(members) == 0 {
		return members, err
	}
	if !node.ready {
		return nil, fmt.Errorf("%w: %s", ErrNodeUnavailable, node.id)
	}
	rows, err = tx.Query(ctx, `SELECT s.id::text,d.id::text FROM subscriptions s
		JOIN user_devices d ON d.user_id=s.user_id
		WHERE s.id=ANY($1::text[]::uuid[]) ORDER BY s.id,d.created_at,d.id`, members)
	if err != nil {
		return nil, err
	}
	peers, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (planPeer, error) {
		var peer planPeer
		err := row.Scan(&peer.subscriptionID, &peer.deviceID)
		return peer, err
	})
	if err != nil {
		return nil, err
	}
	rows, err = tx.Query(ctx, `SELECT address FROM peers WHERE node_id=$1 ORDER BY address`, node.id)
	if err != nil {
		return nil, err
	}
	used, err := pgx.CollectRows(rows, pgx.RowTo[int])
	if err != nil {
		return nil, err
	}
	values := make([][]any, 0, len(peers))
	address := 2
	usedIndex := 0
	for _, peer := range peers {
		for usedIndex < len(used) && used[usedIndex] < address {
			usedIndex++
		}
		for usedIndex < len(used) && used[usedIndex] == address {
			address++
			usedIndex++
		}
		if address > 65534 {
			return nil, fmt.Errorf("%w: %s", ErrAddressPoolFull, node.id)
		}
		private, public, err := vpn.KeyPair()
		if err != nil {
			return nil, err
		}
		values = append(values, []any{peer.subscriptionID, node.id, address, s.Vault.Seal(private), public, peer.deviceID})
		address++
	}
	if _, err := tx.Exec(ctx, `INSERT INTO subscription_nodes(subscription_id,node_id)
		SELECT unnest($1::text[]::uuid[]),$2`, members, node.id); err != nil {
		return nil, err
	}
	if len(values) != 0 {
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"peers"}, []string{"subscription_id", "node_id", "address", "private_key", "public_key", "device_id"}, pgx.CopyFromRows(values)); err != nil {
			return nil, err
		}
	}
	return members, nil
}

func (s Repository) removePlanNode(ctx context.Context, tx pgx.Tx, planID, nodeID string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT sn.subscription_id::text FROM subscription_nodes sn
		JOIN subscriptions s ON s.id=sn.subscription_id
		WHERE s.plan_id=$1 AND sn.node_id=$2 ORDER BY sn.subscription_id`, planID, nodeID)
	if err != nil {
		return nil, err
	}
	members, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(members) == 0 {
		return members, err
	}
	if _, err := tx.Exec(ctx, `WITH removed AS (
		DELETE FROM peers WHERE subscription_id=ANY($1::text[]::uuid[]) AND node_id=$2
		RETURNING subscription_id,last_handshake_at
	), latest AS (SELECT subscription_id,max(last_handshake_at) AS handshake FROM removed GROUP BY subscription_id)
	UPDATE subscriptions s SET removed_peer_last_handshake_at=greatest(s.removed_peer_last_handshake_at,latest.handshake)
	FROM latest WHERE s.id=latest.subscription_id`, members, nodeID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM subscription_nodes WHERE subscription_id=ANY($1::text[]::uuid[]) AND node_id=$2`, members, nodeID); err != nil {
		return nil, err
	}
	return members, nil
}
