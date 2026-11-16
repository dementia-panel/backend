package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"dementia/backend/internal/vpn"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNodeUnavailable = errors.New("node does not exist or is unavailable")
var ErrAddressPoolFull = errors.New("node address pool is full")

type Store struct {
	DB    *pgxpool.Pool
	Vault vpn.Vault
}

type Node struct {
	EndpointHost        *string    `json:"endpoint_host"`
	EndpointPort        *int       `json:"endpoint_port"`
	RetiredAt           *time.Time `json:"retired_at"`
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	IP                  string     `json:"ip"`
	SSHPort             int        `json:"ssh_port"`
	VPNPort             int        `json:"vpn_port"`
	Status              string     `json:"status"`
	Enabled             bool       `json:"enabled"`
	LastSeen            *time.Time `json:"last_seen_at"`
	Revision            int64      `json:"revision"`
	AppliedRevision     int64      `json:"applied_revision"`
	ConnectionOK        *bool      `json:"connection_ok"`
	ConnectionCheckedAt *time.Time `json:"connection_checked_at"`
	Online              bool       `json:"online"`
	RestartRevision     int64      `json:"restart_revision"`
	RestartedRevision   int64      `json:"restarted_revision"`
	OperationStage      string     `json:"operation_stage"`
	Error               *string    `json:"error"`
}

const nodeColumns = "id::text,name,host(ip),ssh_port,vpn_port,status,enabled,last_seen_at,revision,applied_revision,coalesce(last_seen_at>now()-interval '1 minute',false),connection_ok,connection_checked_at,restart_revision,restarted_revision,error,retired_at,endpoint_host,endpoint_port,operation_stage"

func scanNode(row pgx.Row) (Node, error) {
	var n Node
	err := row.Scan(&n.ID, &n.Name, &n.IP, &n.SSHPort, &n.VPNPort, &n.Status, &n.Enabled, &n.LastSeen, &n.Revision, &n.AppliedRevision, &n.Online, &n.ConnectionOK, &n.ConnectionCheckedAt, &n.RestartRevision, &n.RestartedRevision, &n.Error, &n.RetiredAt, &n.EndpointHost, &n.EndpointPort, &n.OperationStage)
	return n, err
}

func NewToken() string {
	var data [32]byte
	rand.Read(data[:])
	return base64.RawURLEncoding.EncodeToString(data[:])
}

func HashToken(token string) []byte {
	hash := sha256.Sum256([]byte(token))
	return hash[:]
}

func (s Store) ReserveNode(ctx context.Context, name, ip, token string, sshPort, vpnPort int) (Node, error) {
	return scanNode(s.DB.QueryRow(ctx, `INSERT INTO nodes(name,ip,host_key,agent_token_hash,ssh_port,vpn_port,operation_stage)
		VALUES($1,$2,'',$3,$4,$5,'queued') ON CONFLICT(ip) DO UPDATE
		SET name=EXCLUDED.name, agent_token_hash=EXCLUDED.agent_token_hash, status='installing',error=NULL,operation_stage='queued',
		    retired_at=NULL,enabled=true,ssh_port=EXCLUDED.ssh_port,vpn_port=EXCLUDED.vpn_port,
		    host_key=CASE WHEN nodes.operation_stage='removed' THEN '' ELSE nodes.host_key END,
		    endpoint_host=CASE WHEN nodes.retired_at IS NOT NULL THEN NULL ELSE nodes.endpoint_host END,
		    endpoint_port=CASE WHEN nodes.retired_at IS NOT NULL THEN NULL ELSE nodes.endpoint_port END,
		    last_seen_at=NULL,handshakes_reported_at=NULL,connection_ok=NULL,connection_checked_at=NULL,applied_revision=0,revision=nodes.revision+1
		WHERE nodes.operation_stage<>'removing' AND
		    (nodes.retired_at IS NOT NULL OR (nodes.status='failed'
		     AND nodes.vpn_port=EXCLUDED.vpn_port AND nodes.ssh_port=EXCLUDED.ssh_port))
		RETURNING `+nodeColumns, name, ip, HashToken(token), sshPort, vpnPort))
}

// TrustNodeHostKey pins the first server key atomically and never replaces a known key.
func (s Store) TrustNodeHostKey(ctx context.Context, id, token, fingerprint string) error {
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

func (s Store) Node(ctx context.Context, id string) (Node, error) {
	return scanNode(s.DB.QueryRow(ctx, "SELECT "+nodeColumns+" FROM nodes WHERE id=$1", id))
}

func (s Store) Nodes(ctx context.Context) ([]Node, error) {
	rows, err := s.DB.Query(ctx, "SELECT "+nodeColumns+" FROM nodes WHERE retired_at IS NULL ORDER BY created_at,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Node{}
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	return result, rows.Err()
}

func (s Store) FinishInstall(ctx context.Context, id, token, publicKey, headerKey, failure string) error {
	if failure != "" {
		_, err := s.DB.Exec(ctx, `UPDATE nodes SET status='failed',error=$3 WHERE id=$1 AND agent_token_hash=$2 AND status='installing'`, id, HashToken(token), failure)
		return err
	}
	_, err := s.DB.Exec(ctx, `UPDATE nodes SET status='ready',operation_stage='complete',public_key=$3,header_key=$4,error=NULL
		WHERE id=$1 AND agent_token_hash=$2 AND status='installing'`, id, HashToken(token), publicKey, s.Vault.Seal(headerKey))
	return err
}

func (s Store) RecoverInstallations(ctx context.Context) error {
	_, err := s.DB.Exec(ctx, `UPDATE nodes SET status=CASE WHEN status='installing' THEN 'failed' ELSE status END,
 error=CASE WHEN operation_stage='removing' THEN 'remote cleanup interrupted; retry removal' ELSE 'installation interrupted; submit credentials again' END,
 operation_stage=CASE WHEN operation_stage='removing' THEN 'remove_failed' ELSE operation_stage END
 WHERE status='installing' OR operation_stage='removing'`)
	return err
}

func (s Store) UpdateNode(ctx context.Context, id string, name *string, enabled *bool) (Node, error) {
	return scanNode(s.DB.QueryRow(ctx, `UPDATE nodes SET name=coalesce($2,name),enabled=coalesce($3,enabled),
	    revision=revision+CASE WHEN enabled IS DISTINCT FROM coalesce($3,enabled) THEN 1 ELSE 0 END
	    WHERE id=$1 AND retired_at IS NULL RETURNING `+nodeColumns, id, name, enabled))
}

func (s Store) RestartNode(ctx context.Context, id string) (Node, error) {
	return scanNode(s.DB.QueryRow(ctx, `UPDATE nodes SET restart_revision=restart_revision+1
	    WHERE id=$1 AND status='ready' AND retired_at IS NULL RETURNING `+nodeColumns, id))
}

type Subscription struct {
	TrafficResetDays *int       `json:"traffic_reset_days"`
	TrafficResetAt   *time.Time `json:"traffic_reset_at"`
	ID               string     `json:"id"`
	UserID           string     `json:"user_id"`
	Name             string     `json:"name"`
	Enabled          bool       `json:"enabled"`
	ExpiresAt        *time.Time `json:"expires_at"`
	TrafficLimit     *int64     `json:"traffic_limit_bytes"`
	TrafficUsed      int64      `json:"traffic_used_bytes"`
	PlanID           *string    `json:"plan_id,omitempty"`
	NodeIDs          []string   `json:"node_ids,omitempty"`
}

func (s Store) CreateSubscription(ctx context.Context, userID, name, token string, expires time.Time, nodeIDs []string) (Subscription, error) {
	var sub Subscription
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		var user string
		if err := tx.QueryRow(ctx, `SELECT id::text FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&user); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO subscriptions(user_id,name,token_hash,expires_at)
			VALUES($1,$2,$3,$4) RETURNING id::text,user_id::text,name,enabled,expires_at`,
			userID, name, HashToken(token), expires).Scan(&sub.ID, &sub.UserID, &sub.Name, &sub.Enabled, &sub.ExpiresAt); err != nil {
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
			if err := s.insertPeer(ctx, tx, sub.ID, nodeID); err != nil {
				return err
			}
		}
		return nil
	})
	sub.NodeIDs = nodeIDs
	return sub, err
}

func (s Store) insertPeer(ctx context.Context, tx pgx.Tx, subscriptionID, nodeID string) error {
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
	if _, err = tx.Exec(ctx, `INSERT INTO peers(subscription_id,node_id,address,private_key,public_key)
		VALUES($1,$2,$3,$4,$5)`, subscriptionID, nodeID, address, s.Vault.Seal(private), public); err != nil {
		return err
	}
	return nil
}

func (s Store) ReplaceSubscriptionNodes(ctx context.Context, subscriptionID string, nodeIDs []string) error {
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		return s.replaceSubscriptionNodes(ctx, tx, subscriptionID, nodeIDs)
	})
}

func (s Store) replaceSubscriptionNodes(ctx context.Context, tx pgx.Tx, subscriptionID string, nodeIDs []string) error {
	var id string
	if err := tx.QueryRow(ctx, "SELECT id::text FROM subscriptions WHERE id=$1 FOR UPDATE", subscriptionID).Scan(&id); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT n.id::text,n.enabled AND n.status='ready',p.node_id IS NOT NULL
		FROM nodes n LEFT JOIN peers p ON p.node_id=n.id AND p.subscription_id=$1
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
			if err := s.insertPeer(ctx, tx, id, item.id); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `WITH removed AS (
			DELETE FROM peers WHERE subscription_id=$1 AND node_id=$2 RETURNING last_handshake_at
		) UPDATE subscriptions SET removed_peer_last_handshake_at=greatest(
			removed_peer_last_handshake_at,(SELECT last_handshake_at FROM removed)) WHERE id=$1`, id, item.id); err != nil {
			return err
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

func (s Store) Subscriptions(ctx context.Context) ([]Subscription, error) {
	rows, err := s.DB.Query(ctx, `SELECT s.id::text,s.user_id::text,s.name,s.enabled,s.expires_at,s.traffic_limit_bytes,s.traffic_used_bytes,s.traffic_reset_days,s.traffic_reset_at,s.plan_id::text,
		ARRAY(SELECT p.node_id::text FROM peers p WHERE p.subscription_id=s.id ORDER BY p.node_id)
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

func (s Store) UpdateSubscription(ctx context.Context, id string, enabled *bool, expiry *time.Time) error {
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE subscriptions SET enabled=coalesce($2,enabled),expires_at=coalesce($3,expires_at) WHERE id=$1`, id, enabled, expiry)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return bumpNodes(ctx, tx, `SELECT node_id FROM peers WHERE subscription_id=$1`, id)
	})
}

func bumpNodes(ctx context.Context, tx pgx.Tx, selection, id string) error {
	_, err := tx.Exec(ctx, `WITH affected AS (SELECT id FROM nodes WHERE id IN (`+selection+`) ORDER BY id FOR UPDATE)
		UPDATE nodes SET revision=revision+1 FROM affected WHERE nodes.id=affected.id`, id)
	return err
}

func (s Store) RotateToken(ctx context.Context, id, token string) error {
	tag, err := s.DB.Exec(ctx, `UPDATE subscriptions SET token_hash=$2 WHERE id=$1`, id, HashToken(token))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

type Location struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
}

func (s Store) PublicSubscription(ctx context.Context, token string) (Subscription, []Location, error) {
	var sub Subscription
	locations := []Location{}
	err := pgx.BeginTxFunc(ctx, s.DB, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT s.id::text,s.name,s.enabled,s.expires_at
			FROM subscriptions s JOIN users u ON u.id=s.user_id
			WHERE s.token_hash=$1 AND s.enabled AND u.enabled AND (s.expires_at IS NULL OR s.expires_at>now()) AND (s.traffic_limit_bytes IS NULL OR s.traffic_used_bytes<s.traffic_limit_bytes)`, HashToken(token)).
			Scan(&sub.ID, &sub.Name, &sub.Enabled, &sub.ExpiresAt)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT n.id::text,n.name,coalesce(n.enabled AND n.status='ready' AND n.applied_revision=n.revision AND n.last_seen_at>now()-interval '1 minute',false)
			FROM peers p JOIN nodes n ON n.id=p.node_id WHERE p.subscription_id=$1 ORDER BY n.name,n.id`, sub.ID)
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

const syncedProfile = `n.applied_revision=n.revision AND n.last_seen_at>now()-interval '1 minute'`

const clientProfilesQuery = `SELECT n.name,coalesce(n.endpoint_host,host(n.ip)),coalesce(n.endpoint_port,n.vpn_port),n.public_key,n.header_key,p.private_key,p.address
	FROM subscriptions s JOIN users u ON u.id=s.user_id JOIN peers p ON p.subscription_id=s.id JOIN nodes n ON n.id=p.node_id
	WHERE s.enabled AND u.enabled AND (s.expires_at IS NULL OR s.expires_at>now()) AND (s.traffic_limit_bytes IS NULL OR s.traffic_used_bytes<s.traffic_limit_bytes)
	    AND n.enabled AND n.status='ready' AND n.retired_at IS NULL AND `

func (s Store) Profile(ctx context.Context, token, nodeID string) (vpn.ClientProfile, error) {
	return s.clientProfile(s.DB.QueryRow(ctx, clientProfilesQuery+`n.id=$1 AND s.token_hash=$2 AND `+syncedProfile, nodeID, HashToken(token)))
}

func (s Store) ProfileForSubscription(ctx context.Context, subscriptionID, nodeID string) (vpn.ClientProfile, error) {
	// Administrators can export the issued profile while it is syncing to the node.
	return s.clientProfile(s.DB.QueryRow(ctx, clientProfilesQuery+`n.id=$1 AND s.id=$2`, nodeID, subscriptionID))
}

func (s Store) PublicProfiles(ctx context.Context, token string) ([]vpn.ClientProfile, error) {
	rows, err := s.DB.Query(ctx, clientProfilesQuery+`s.token_hash=$1 AND `+syncedProfile+` ORDER BY n.name,n.id`, HashToken(token))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var profiles []vpn.ClientProfile
	for rows.Next() {
		profile, err := s.clientProfile(rows)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, rows.Err()
}

func (s Store) clientProfile(row pgx.Row) (vpn.ClientProfile, error) {
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
	return vpn.ClientProfile{
		Name: name, PrivateKey: privateKey, PublicKey: public, HeaderKey: headerKey,
		IP: ip, Port: port, Address: vpn.Address(address),
	}, nil
}

type Peer struct {
	PublicKey string    `json:"public_key"`
	Address   string    `json:"address"`
	ExpiresAt time.Time `json:"expires_at"`
}

type State struct {
	Revision        int64  `json:"revision"`
	Peers           []Peer `json:"peers"`
	Name            string `json:"name"`
	RestartRevision int64  `json:"restart_revision"`
}

func (s Store) State(ctx context.Context, id, token string) (State, error) {
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
			p.Address = vpn.Address(addr)
			state.Peers = append(state.Peers, p)
		}
		return rows.Err()
	})
	return state, err
}

func (s Store) AgentExists(ctx context.Context, id, token string) (bool, error) {
	var exists bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM nodes WHERE id=$1 AND agent_token_hash=$2 AND status='ready' AND retired_at IS NULL
	)`, id, HashToken(token)).Scan(&exists)
	return exists, err
}

func (s Store) Ack(ctx context.Context, id, token string, revision, restarted int64, handshakes map[string]int64, traffic map[string]TrafficCounter) error {
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if len(traffic) > 0 {
			// Match the lock order used by subscription mutations.
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
		if err := recordTraffic(ctx, tx, id, traffic); err != nil {
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
