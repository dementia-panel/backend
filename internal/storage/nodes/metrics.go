package nodes

import (
	"context"
	. "dementia/backend/internal/storage/shared"
)

const onlineUsersQuery = `SELECT count(DISTINCT u.id)::integer
	FROM peers p JOIN subscriptions s ON s.id=p.subscription_id
	JOIN users u ON u.id=s.user_id JOIN nodes n ON n.id=p.node_id
	WHERE u.enabled AND s.enabled AND (s.expires_at IS NULL OR s.expires_at>now())
	AND (s.traffic_limit_bytes IS NULL OR s.traffic_used_bytes<s.traffic_limit_bytes)
	AND n.enabled AND n.status='ready' AND n.retired_at IS NULL AND n.applied_revision=n.revision
	AND n.handshakes_reported_at>now()-interval '1 minute'
	AND p.current_handshake_at>now()-interval '3 minutes'`

func (s Repository) NodeMetrics(ctx context.Context, _ string) (NodeMetrics, error) {
	var metrics NodeMetrics
	err := s.DB.QueryRow(ctx, `WITH online AS (`+onlineUsersQuery+`)
		SELECT online.count,
			greatest(online.count,coalesce((SELECT users_count FROM node_online_peak WHERE id=true),0)),
			coalesce((SELECT sum(received_bytes+sent_bytes)::bigint FROM traffic_daily
			WHERE day=(now() AT TIME ZONE 'UTC')::date),0)
		FROM online`).Scan(&metrics.OnlineUsers, &metrics.PeakOnlineUsers, &metrics.TrafficTodayBytes)
	return metrics, err
}

func (s Repository) SampleOnlinePeak(ctx context.Context, _ string) error {
	_, err := s.DB.Exec(ctx, `INSERT INTO node_online_peak(id,users_count)
		SELECT true,online.count FROM (`+onlineUsersQuery+`) online WHERE true
		ON CONFLICT(id) DO UPDATE SET users_count=greatest(node_online_peak.users_count,excluded.users_count)`)
	return err
}
