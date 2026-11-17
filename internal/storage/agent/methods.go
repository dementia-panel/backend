package agent

import (
	"context"
	"dementia/backend/internal/lib/logger/with"
	. "dementia/backend/internal/storage/shared"
	"dementia/backend/internal/vpn"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

func (s Repository) State(ctx context.Context, requestID string, id, token string) (State, error) {
	const op = "internal/storage/agent/State"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	state := State{Peers: []Peer{}}
	err := pgx.BeginTxFunc(ctx, s.DB, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var enabled bool
		err := tx.QueryRow(ctx, `SELECT revision,enabled,name,restart_revision FROM nodes WHERE id=$1 AND agent_token_hash=$2 AND status='ready' AND retired_at IS NULL`, id, HashToken(token)).Scan(&state.Revision, &enabled, &state.Name, &state.RestartRevision)
		if err != nil || !enabled {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT p.public_key,p.address,coalesce(s.expires_at,'9999-12-31 23:59:59+00'::timestamptz)
			FROM peers p JOIN subscriptions s ON s.id=p.subscription_id JOIN users u ON u.id=s.user_id
			WHERE p.node_id=$1 AND s.enabled AND u.enabled AND (s.expires_at IS NULL OR s.expires_at>now()) AND (s.traffic_limit_bytes IS NULL OR s.traffic_used_bytes<s.traffic_limit_bytes) ORDER BY p.address`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p Peer
			var addr int
			if err := rows.Scan(&p.PublicKey, &addr, &p.ExpiresAt); err != nil {
				return err
			}
			p.ExpiresAt = p.ExpiresAt.UTC()
			p.Address = vpn.Address(addr)
			state.Peers = append(state.Peers, p)
		}
		return rows.Err()
	})
	return state, err
}
func (s Repository) AgentExists(ctx context.Context, requestID string, id, token string) (bool, error) {
	const op = "internal/storage/agent/AgentExists"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	var exists bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM nodes WHERE id=$1 AND agent_token_hash=$2 AND status='ready' AND retired_at IS NULL
	)`, id, HashToken(token)).Scan(&exists)
	return exists, err
}
func (s Repository) Ack(ctx context.Context, requestID string, id, token string, revision, restarted int64, handshakes map[string]int64, traffic map[string]TrafficCounter) error {
	const op = "internal/storage/agent/Ack"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if len(traffic) > 0 {
			if _, err := tx.Exec(ctx, `SELECT s.id FROM subscriptions s WHERE s.id IN (
				SELECT p.subscription_id FROM peers p JOIN jsonb_object_keys($2::jsonb) k ON k=p.public_key
				WHERE p.node_id=$1 AND EXISTS (
					SELECT 1 FROM nodes WHERE id=$1 AND agent_token_hash=$3 AND status='ready' AND retired_at IS NULL
				)
			) ORDER BY s.id FOR UPDATE`, id, traffic, HashToken(token)); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE nodes SET applied_revision=$3,last_seen_at=clock_timestamp(),
		restarted_revision=greatest(restarted_revision,$4),
		handshakes_reported_at=CASE WHEN $5 THEN clock_timestamp() ELSE handshakes_reported_at END
		WHERE id=$1 AND agent_token_hash=$2 AND status='ready' AND retired_at IS NULL AND $3 BETWEEN applied_revision AND revision AND $4<=restart_revision`, id, HashToken(token), revision, restarted, handshakes != nil)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		if err := RecordTraffic(ctx, requestID, tx, id, traffic); err != nil {
			return err
		}
		if handshakes == nil {
			return nil
		}
		_, err = tx.Exec(ctx, `WITH reported AS (
		SELECT key, to_timestamp(value::bigint) AS handshake
		FROM jsonb_each_text($2::jsonb)
		WHERE value::bigint BETWEEN 1 AND extract(epoch FROM now() + interval '30 seconds')
	)
	UPDATE peers p
	SET current_handshake_at=r.handshake,
	    last_handshake_at=greatest(p.last_handshake_at,r.handshake)
	FROM peers existing LEFT JOIN reported r ON r.key=existing.public_key
	WHERE p.public_key=existing.public_key AND existing.node_id=$1
	    AND p.current_handshake_at IS DISTINCT FROM r.handshake`, id, handshakes)
		return err
	})
}
