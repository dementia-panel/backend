package nodes

import (
	"context"
	"dementia/backend/internal/lib/logger/with"
	. "dementia/backend/internal/storage/shared"
	"errors"
	"log/slog"
)

func (s Repository) ReserveNode(ctx context.Context, requestID string, name, ip, token string, sshPort, vpnPort int, resetHostKey ...bool) (Node, error) {
	const op = "internal/storage/nodes/ReserveNode"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return ScanNode(s.DB.QueryRow(ctx, `INSERT INTO nodes(name,ip,host_key,agent_token_hash,ssh_port,vpn_port,operation_stage)
		VALUES($1,$2,'',$3,$4,$5,'queued') ON CONFLICT(ip) DO UPDATE
		SET name=EXCLUDED.name, agent_token_hash=EXCLUDED.agent_token_hash, status='installing',error=NULL,operation_stage='queued',
		    retired_at=NULL,enabled=true,ssh_port=EXCLUDED.ssh_port,vpn_port=EXCLUDED.vpn_port,
		    host_key=CASE WHEN nodes.operation_stage='removed' OR $6 THEN '' ELSE nodes.host_key END,
		    endpoint_host=CASE WHEN nodes.retired_at IS NOT NULL THEN NULL ELSE nodes.endpoint_host END,
		    endpoint_port=CASE WHEN nodes.retired_at IS NOT NULL THEN NULL ELSE nodes.endpoint_port END,
		    last_seen_at=NULL,handshakes_reported_at=NULL,connection_ok=NULL,connection_checked_at=NULL,applied_revision=0,revision=nodes.revision+1,agent_version=''
		WHERE nodes.operation_stage<>'removing' AND
		    (nodes.retired_at IS NOT NULL OR (nodes.status='failed'
		     AND nodes.vpn_port=EXCLUDED.vpn_port AND nodes.ssh_port=EXCLUDED.ssh_port))
		RETURNING `+NodeProvisionColumns, name, ip, HashToken(token), sshPort, vpnPort, len(resetHostKey) > 0 && resetHostKey[0]))
}
func (s Repository) TrustNodeHostKey(ctx context.Context, requestID string, id, token, fingerprint string) error {
	const op = "internal/storage/nodes/TrustNodeHostKey"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	tag, err := s.DB.Exec(ctx, `UPDATE nodes SET host_key=$3
        WHERE id=$1 AND agent_token_hash=$2 AND status='installing' AND retired_at IS NULL
          AND (host_key='' OR host_key=$3)`, id, HashToken(token), fingerprint)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("SSH server key changed or installation is no longer active")
	}
	return nil
}
func (s Repository) Node(ctx context.Context, requestID string, id string) (Node, error) {
	const op = "internal/storage/nodes/Node"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return ScanNode(s.DB.QueryRow(ctx, "SELECT "+NodeColumns+" FROM nodes WHERE id=$1", id))
}
func (s Repository) Nodes(ctx context.Context, requestID string) ([]Node, error) {
	const op = "internal/storage/nodes/Nodes"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	rows, err := s.DB.Query(ctx, "SELECT "+NodeColumns+" FROM nodes WHERE retired_at IS NULL ORDER BY created_at,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Node{}
	for rows.Next() {
		n, err := ScanNode(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	return result, rows.Err()
}
func (s Repository) FinishInstall(ctx context.Context, requestID string, id, token, publicKey, headerKey, failure string) error {
	const op = "internal/storage/nodes/FinishInstall"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	if failure != "" {
		_, err := s.DB.Exec(ctx, `UPDATE nodes SET status='failed',error=$3 WHERE id=$1 AND agent_token_hash=$2 AND status='installing'`, id, HashToken(token), failure)
		return err
	}
	_, err := s.DB.Exec(ctx, `UPDATE nodes SET status='ready',operation_stage='complete',public_key=$3,header_key=$4,error=NULL
		WHERE id=$1 AND agent_token_hash=$2 AND status='installing'`, id, HashToken(token), publicKey, s.Vault.Seal(headerKey))
	return err
}
func (s Repository) RecoverInstallations(ctx context.Context, requestID string) error {
	const op = "internal/storage/nodes/RecoverInstallations"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	_, err := s.DB.Exec(ctx, `UPDATE nodes SET status=CASE WHEN status='installing' THEN 'failed' ELSE status END,
 error=CASE WHEN operation_stage='removing' THEN 'remote cleanup interrupted; retry removal'
            WHEN operation_stage='upgrading' THEN 'node update interrupted; retry update'
            ELSE 'installation interrupted; submit credentials again' END,
 operation_stage=CASE WHEN operation_stage='removing' THEN 'remove_failed'
                      WHEN operation_stage='upgrading' THEN 'upgrade_failed' ELSE operation_stage END
 WHERE status='installing' OR operation_stage IN ('removing','upgrading')`)
	return err
}

func (s Repository) BeginNodeUpgrade(ctx context.Context, requestID, id string) (Node, error) {
	return ScanNode(s.DB.QueryRow(ctx, `UPDATE nodes SET operation_stage='upgrading',error=NULL
        WHERE id=$1 AND status='ready' AND retired_at IS NULL
          AND operation_stage NOT IN ('upgrading','removing') RETURNING `+NodeColumns, id))
}

func (s Repository) VerifyUpgradeHostKey(ctx context.Context, requestID, id, key string) error {
	var trusted bool
	err := s.DB.QueryRow(ctx, `SELECT host_key<>'' AND host_key=$2 FROM nodes
        WHERE id=$1 AND operation_stage='upgrading'`, id, key).Scan(&trusted)
	if err != nil {
		return err
	}
	if !trusted {
		return ErrNodeUnavailable
	}
	return nil
}

func (s Repository) FinishNodeUpgrade(ctx context.Context, requestID, id, failure string) error {
	stage := "complete"
	if failure != "" {
		stage = "upgrade_failed"
	}
	_, err := s.DB.Exec(ctx, `UPDATE nodes SET operation_stage=$2,error=nullif($3,'')
        WHERE id=$1 AND operation_stage='upgrading'`, id, stage, failure)
	return err
}
func (s Repository) UpdateNode(ctx context.Context, requestID string, id string, name *string, enabled *bool) (Node, error) {
	const op = "internal/storage/nodes/UpdateNode"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return ScanNode(s.DB.QueryRow(ctx, `UPDATE nodes SET name=coalesce($2,name),enabled=coalesce($3,enabled),
	    revision=revision+CASE WHEN enabled IS DISTINCT FROM coalesce($3,enabled) THEN 1 ELSE 0 END
	    WHERE id=$1 AND retired_at IS NULL RETURNING `+NodeColumns, id, name, enabled))
}
func (s Repository) RestartNode(ctx context.Context, requestID string, id string) (Node, error) {
	const op = "internal/storage/nodes/RestartNode"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return ScanNode(s.DB.QueryRow(ctx, `UPDATE nodes SET restart_revision=restart_revision+1
	    WHERE id=$1 AND status='ready' AND retired_at IS NULL RETURNING `+NodeColumns, id))
}
