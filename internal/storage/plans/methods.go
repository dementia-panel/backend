package plans

import (
	"context"
	"dementia/backend/internal/lib/logger/with"
	. "dementia/backend/internal/storage/shared"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s Repository) SubscriptionPlans(ctx context.Context, requestID string) ([]SubscriptionPlan, error) {
	const op = "internal/storage/plans/SubscriptionPlans"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	rows, err := s.DB.Query(ctx, `SELECT p.id::text,p.name,ARRAY(SELECT node_id::text FROM subscription_plan_nodes WHERE plan_id=p.id ORDER BY node_id)
		FROM subscription_plans p ORDER BY p.name,p.id`)
	if err != nil {
		return nil, err
	}
	plans, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (SubscriptionPlan, error) {
		var plan SubscriptionPlan
		err := row.Scan(&plan.ID, &plan.Name, &plan.NodeIDs)
		return plan, err
	})
	if plans == nil && err == nil {
		plans = []SubscriptionPlan{}
	}
	return plans, err
}
func (s Repository) SaveSubscriptionPlan(ctx context.Context, requestID string, id, name string, nodeIDs []string) (SubscriptionPlan, error) {
	const op = "internal/storage/plans/SaveSubscriptionPlan"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	plan := SubscriptionPlan{ID: id, Name: name, NodeIDs: nodeIDs}
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if id == "" {
			if err := tx.QueryRow(ctx, `INSERT INTO subscription_plans(name) VALUES($1) RETURNING id::text`, name).Scan(&plan.ID); err != nil {
				return err
			}
		} else {
			tag, err := tx.Exec(ctx, `UPDATE subscription_plans SET name=$2 WHERE id=$1`, id, name)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return pgx.ErrNoRows
			}
		}
		rows, err := tx.Query(ctx, `UPDATE subscriptions SET name=$2 WHERE plan_id=$1 RETURNING id::text`, plan.ID, name)
		if err != nil {
			return err
		}
		accessIDs, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		var valid int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM (SELECT id FROM nodes WHERE id=ANY($1::text[]::uuid[]) AND retired_at IS NULL ORDER BY id FOR UPDATE) n`, nodeIDs).Scan(&valid); err != nil {
			return err
		}
		if valid != len(nodeIDs) {
			return ErrNodeUnavailable
		}
		if _, err := tx.Exec(ctx, `DELETE FROM subscription_plan_nodes WHERE plan_id=$1`, plan.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO subscription_plan_nodes(plan_id,node_id) SELECT $1,unnest($2::text[]::uuid[])`, plan.ID, nodeIDs); err != nil {
			return err
		}
		for _, accessID := range accessIDs {
			if err := s.ReplaceSubscriptionNodesTx(ctx, requestID, tx, accessID, nodeIDs); err != nil {
				return err
			}
		}
		return nil
	})
	return plan, err
}
func (s Repository) DeleteSubscriptionPlan(ctx context.Context, requestID string, id string) error {
	const op = "internal/storage/plans/DeleteSubscriptionPlan"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		var planID string
		if err := tx.QueryRow(ctx, `SELECT id::text FROM subscription_plans WHERE id=$1 FOR UPDATE`, id).Scan(&planID); err != nil {
			return err
		}
		var assigned bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE subscription_plan_id=$1) OR EXISTS(SELECT 1 FROM subscriptions WHERE plan_id=$1 AND enabled)`, id).Scan(&assigned); err != nil {
			return err
		}
		if assigned {
			return ErrPlanInUse
		}
		if _, err := tx.Exec(ctx, `UPDATE subscriptions SET plan_id=NULL WHERE plan_id=$1`, id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM subscription_plans WHERE id=$1`, id)
		return err
	})
}
func (s Repository) AssignSubscriptionPlan(ctx context.Context, requestID string, userID string, planID *string, token string) (id string, issued bool, err error) {
	const op = "internal/storage/plans/AssignSubscriptionPlan"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return s.AssignSubscriptionPlanAccess(ctx, requestID, userID, planID, token, nil, nil, false)
}
func (s Repository) AssignSubscriptionPlanAccess(ctx context.Context, requestID string, userID string, planID *string, token string, enabled *bool, expiresAt *time.Time, setExpiry bool) (id string, issued bool, err error) {
	const op = "internal/storage/plans/AssignSubscriptionPlanAccess"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	err = pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		id, issued, err = s.AssignSubscriptionPlanTx(ctx, requestID, tx, userID, planID, token)
		if err != nil || id == "" || (enabled == nil && !setExpiry) {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE subscriptions SET enabled=coalesce($2,enabled),
			expires_at=CASE WHEN $3 THEN $4 ELSE expires_at END WHERE id=$1`, id, enabled, setExpiry, expiresAt); err != nil {
			return err
		}
		return BumpNodes(ctx, requestID, tx, `SELECT node_id FROM peers WHERE subscription_id=$1`, id)
	})
	return
}
func (s Repository) AssignedSubscription(ctx context.Context, requestID string, userID string) (*Subscription, error) {
	const op = "internal/storage/plans/AssignedSubscription"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	var sub Subscription
	err := s.DB.QueryRow(ctx, `SELECT s.id::text,s.user_id::text,s.name,s.enabled,s.expires_at,
		s.traffic_limit_bytes,s.traffic_used_bytes,s.plan_id::text,
		ARRAY(SELECT node_id::text FROM subscription_nodes WHERE subscription_id=s.id ORDER BY node_id)
		FROM users u JOIN subscriptions s ON s.user_id=u.id AND s.plan_id=u.subscription_plan_id
		WHERE u.id=$1`, userID).Scan(&sub.ID, &sub.UserID, &sub.Name, &sub.Enabled, &sub.ExpiresAt, &sub.TrafficLimit, &sub.TrafficUsed, &sub.PlanID, &sub.NodeIDs)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		var assigned bool
		if checkErr := s.DB.QueryRow(ctx, `SELECT subscription_plan_id IS NOT NULL FROM users WHERE id=$1`, userID).Scan(&assigned); checkErr != nil {
			return nil, checkErr
		}
		if !assigned {
			return nil, nil
		}
		return nil, err
	}
	return &sub, nil
}
func (s Repository) AssignSubscriptionPlanTx(ctx context.Context, requestID string, tx pgx.Tx, userID string, planID *string, token string) (id string, issued bool, err error) {
	const op = "internal/storage/plans/AssignSubscriptionPlanTx"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	apply := func() error {
		var current *string
		if err := tx.QueryRow(ctx, `SELECT subscription_plan_id::text FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&current); err != nil {
			return err
		}
		var nodeIDs []string
		if planID != nil {
			if err := s.EnsureInitialDevice(ctx, requestID, tx, userID); err != nil {
				return err
			}
			var name string
			if err := tx.QueryRow(ctx, `SELECT name,ARRAY(SELECT node_id::text FROM subscription_plan_nodes WHERE plan_id=$1 ORDER BY node_id)
				FROM subscription_plans WHERE id=$1 FOR UPDATE`, *planID).Scan(&name, &nodeIDs); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `INSERT INTO subscriptions (user_id,plan_id,name,token_hash,expires_at,token_encrypted) VALUES($1,$2,$3,$4,NULL,$5)
				ON CONFLICT (user_id,plan_id) WHERE plan_id IS NOT NULL
				DO UPDATE SET enabled=true,name=EXCLUDED.name RETURNING id::text,token_hash=$4`, userID, *planID, name, HashToken(token), s.Vault.Seal(token)).Scan(&id, &issued); err != nil {
				return err
			}
			if err := s.ReplaceSubscriptionNodesTx(ctx, requestID, tx, id, nodeIDs); err != nil {
				return err
			}
		}
		if (current == nil && planID != nil) || (current != nil && (planID == nil || *planID != *current)) {
			if _, err := tx.Exec(ctx, `UPDATE subscriptions SET enabled=false WHERE user_id=$1 AND id::text<>$2 AND enabled`, userID, id); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE users SET subscription_plan_id=$2,updated_at=now() WHERE id=$1`, userID, planID); err != nil {
				return err
			}
			return BumpNodes(ctx, requestID, tx, `SELECT p.node_id FROM peers p JOIN subscriptions s ON s.id=p.subscription_id WHERE s.user_id=$1`, userID)
		}
		if id != "" {
			return BumpNodes(ctx, requestID, tx, "SELECT node_id FROM peers WHERE subscription_id=$1", id)
		}
		return nil
	}
	err = apply()
	return
}
