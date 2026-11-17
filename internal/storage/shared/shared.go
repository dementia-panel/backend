package shared

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"dementia/backend/internal/lib/logger/with"
	"dementia/backend/internal/vpn"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Connection struct {
	DB    *pgxpool.Pool
	Vault vpn.Vault
}
type SubscriptionBranding struct {
	DefaultLanguage string `json:"default_language"`
	Name            string `json:"name"`
	Logo            string `json:"logo"`
}

var ErrDeviceLimit = errors.New("device limit reached")
var ErrAmbiguousUser = errors.New("telegram_id belongs to multiple users; use UUID")

type Device struct {
	ID              string     `json:"id"`
	UserID          string     `json:"user_id"`
	Name            string     `json:"name"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	LastConnectedAt *time.Time `json:"last_connected_at"`
	Online          bool       `json:"online"`
}

const SelectDevices = `SELECT d.id::text,d.user_id::text,d.name,d.created_at,d.updated_at,max(p.last_handshake_at),
 coalesce(bool_or(u.enabled AND s.enabled AND (s.expires_at IS NULL OR s.expires_at>now())
 AND (s.traffic_limit_bytes IS NULL OR s.traffic_used_bytes<s.traffic_limit_bytes)
 AND n.enabled AND n.status='ready' AND n.retired_at IS NULL AND n.applied_revision=n.revision
 AND n.handshakes_reported_at>now()-interval '1 minute' AND p.current_handshake_at>now()-interval '3 minutes'),false)
 FROM user_devices d JOIN users u ON u.id=d.user_id LEFT JOIN peers p ON p.device_id=d.id
 LEFT JOIN subscriptions s ON s.id=p.subscription_id LEFT JOIN nodes n ON n.id=p.node_id`

func LockDeviceUser(ctx context.Context, requestID string, tx pgx.Tx, userID string) error {
	const op = "internal/storage/shared/LockDeviceUser"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(ctx, "repository transaction")
	var id string
	return tx.QueryRow(ctx, `SELECT id::text FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&id)
}

type DeviceServer struct {
	DeviceID        string     `json:"-"`
	NodeID          string     `json:"node_id"`
	LastConnectedAt *time.Time `json:"last_connected_at"`
}
type DeviceOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type ConnectionCheck struct {
	Started time.Time
	NodeIDs []string
}
type ConnectionChange struct {
	ID     string
	Name   string
	Online bool
}
type Event struct {
	ID        int64           `json:"id"`
	Scope     string          `json:"scope"`
	Name      string          `json:"event"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
	Attempts  int             `json:"-"`
}

func RemoveNodeData(ctx context.Context, requestID string, tx pgx.Tx, id string, localOnly bool) error {
	const op = "internal/storage/shared/RemoveNodeData"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(ctx, "repository transaction")
	if _, err := tx.Exec(ctx, `SELECT s.id FROM subscriptions s WHERE EXISTS (
            SELECT 1 FROM subscription_nodes sn WHERE sn.subscription_id=s.id AND sn.node_id=$1)
            ORDER BY s.id FOR UPDATE`, id); err != nil {
		return err
	}
	var name string
	if err := tx.QueryRow(ctx, `UPDATE nodes SET retired_at=now(),enabled=false,operation_stage='removed',error=NULL,revision=revision+1
			WHERE id=$1 AND retired_at IS NULL AND (($2 AND status<>'installing' AND operation_stage<>'removing') OR (NOT $2 AND operation_stage='removing')) RETURNING name`, id, localOnly).Scan(&name); err != nil {
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
	return RecordDeletion(ctx, requestID, tx, "node", id, name)
}
func RecordDeletion(ctx context.Context, requestID string, tx pgx.Tx, scope, id, name string) error {
	const op = "internal/storage/shared/RecordDeletion"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(ctx, "repository transaction")
	_, err := tx.Exec(ctx, `INSERT INTO events(scope,event,data)
		SELECT $1::text,$1::text||'.deleted',jsonb_build_object('id',$2::text,'name',$3::text)
		WHERE current_setting('dementia.webhook_enabled',true)='on'
		OR current_setting('dementia.telegram_enabled',true)='on'`, scope, id, name)
	return err
}

type SubscriptionPlan struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	NodeIDs []string `json:"node_ids"`
}

var ErrPlanInUse = errors.New("subscription plan is in use")

type TelegramNotification struct {
	ID       int64
	Event    string
	Data     json.RawMessage
	Attempts int
}
type TelegramNodeMetric struct {
	Name     string
	Status   string
	Enabled  bool
	Online   bool
	Received int64
	Sent     int64
}
type TelegramSettings struct {
	Node         bool `json:"node"`
	User         bool `json:"user"`
	Admin        bool `json:"admin"`
	APIToken     bool `json:"api_token"`
	Subscription bool `json:"subscription"`
}
type User struct {
	ID                 string     `json:"id"`
	Username           string     `json:"username"`
	TelegramID         *int64     `json:"telegram_id"`
	Enabled            bool       `json:"enabled"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	LastConnectedAt    *time.Time `json:"last_connected_at"`
	SubscriptionPlanID *string    `json:"subscription_plan_id"`
	Online             bool       `json:"online"`
	DeviceLimit        *int       `json:"device_limit"`
	DeviceCount        int        `json:"device_count"`
	OnlineDeviceCount  int        `json:"online_device_count"`
}

func ScanUser(row pgx.Row) (User, error) {
	var user User
	err := row.Scan(&user.ID, &user.Username, &user.TelegramID, &user.Enabled, &user.CreatedAt, &user.UpdatedAt, &user.LastConnectedAt, &user.Online, &user.SubscriptionPlanID, &user.DeviceLimit, &user.DeviceCount, &user.OnlineDeviceCount)
	return user, err
}

const SelectUsers = `SELECT u.id::text,u.username,u.telegram_id,u.enabled,u.created_at,u.updated_at,
	activity.last_connected_at,coalesce(activity.online,false) AS online,u.subscription_plan_id::text,u.device_limit,
 (SELECT count(*) FROM user_devices d WHERE d.user_id=u.id) AS device_count,coalesce(activity.online_devices,0) AS online_device_count
	FROM users u
	CROSS JOIN LATERAL (
		SELECT greatest(max(p.last_handshake_at),max(s.removed_peer_last_handshake_at)) AS last_connected_at,
		    bool_or(u.enabled AND s.enabled AND (s.expires_at IS NULL OR s.expires_at>now()) AND (s.traffic_limit_bytes IS NULL OR s.traffic_used_bytes<s.traffic_limit_bytes) AND n.enabled AND n.status='ready'
		        AND n.applied_revision=n.revision
		        AND n.handshakes_reported_at>now()-interval '1 minute'
		        AND p.current_handshake_at>now()-interval '3 minutes') AS online,
 count(DISTINCT p.device_id) FILTER (WHERE u.enabled AND s.enabled AND (s.expires_at IS NULL OR s.expires_at>now())
 AND (s.traffic_limit_bytes IS NULL OR s.traffic_used_bytes<s.traffic_limit_bytes) AND n.enabled AND n.status='ready'
 AND n.applied_revision=n.revision AND n.handshakes_reported_at>now()-interval '1 minute'
 AND p.current_handshake_at>now()-interval '3 minutes') AS online_devices
		FROM subscriptions s LEFT JOIN peers p ON p.subscription_id=s.id LEFT JOIN nodes n ON n.id=p.node_id
		WHERE s.user_id=u.id
	) activity`

func InsertAdminSession(ctx context.Context, requestID string, tx pgx.Tx, issue func(string) (string, time.Time, error)) error {
	const op = "internal/storage/shared/InsertAdminSession"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(ctx, "repository transaction")
	var id string
	if err := tx.QueryRow(ctx, "SELECT gen_random_uuid()::text").Scan(&id); err != nil {
		return err
	}
	refresh, expires, err := issue(id)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO admin_sessions(session_id,token_hash,administrator_id,expires_at)
		VALUES($1,$2,true,$3)`, id, HashToken(refresh), expires)
	return err
}

var ErrNodeUnavailable = errors.New("node does not exist or is unavailable")
var ErrAddressPoolFull = errors.New("node address pool is full")

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

const NodeColumns = "id::text,name,host(ip),ssh_port,vpn_port,status,enabled,last_seen_at,revision,applied_revision,coalesce(last_seen_at>now()-interval '1 minute',false),connection_ok,connection_checked_at,restart_revision,restarted_revision,error,retired_at,endpoint_host,endpoint_port,operation_stage"

func ScanNode(row pgx.Row) (Node, error) {
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

func BumpNodes(ctx context.Context, requestID string, tx pgx.Tx, selection, id string) error {
	const op = "internal/storage/shared/BumpNodes"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(ctx, "repository transaction")
	_, err := tx.Exec(ctx, `WITH affected AS (SELECT id FROM nodes WHERE id IN (`+selection+`) ORDER BY id FOR UPDATE)
		UPDATE nodes SET revision=revision+1 FROM affected WHERE nodes.id=affected.id`, id)
	return err
}

type Location struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
}

const SyncedProfile = `n.applied_revision=n.revision AND n.last_seen_at>now()-interval '1 minute'`
const ClientProfilesQuery = `SELECT n.name,coalesce(n.endpoint_host,host(n.ip)),coalesce(n.endpoint_port,n.vpn_port),n.public_key,n.header_key,p.private_key,p.address
	FROM subscriptions s JOIN users u ON u.id=s.user_id JOIN peers p ON p.subscription_id=s.id JOIN nodes n ON n.id=p.node_id
	WHERE s.enabled AND u.enabled AND (s.expires_at IS NULL OR s.expires_at>now()) AND (s.traffic_limit_bytes IS NULL OR s.traffic_used_bytes<s.traffic_limit_bytes)
	    AND n.enabled AND n.status='ready' AND n.retired_at IS NULL AND `

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

func FirstDeviceID(ids []string) string {
	if len(ids) > 0 {
		return ids[0]
	}
	return ""
}

type TrafficCounter struct {
	Received int64 `json:"received"`
	Sent     int64 `json:"sent"`
}

func RecordTraffic(ctx context.Context, requestID string, tx pgx.Tx, nodeID string, counters map[string]TrafficCounter) error {
	const op = "internal/storage/shared/RecordTraffic"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(ctx, "repository transaction")
	if len(counters) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `WITH samples AS (
		SELECT key,(value->>'received')::bigint AS rx,(value->>'sent')::bigint AS tx
		FROM jsonb_each($2::jsonb)
	), deltas AS MATERIALIZED (
		SELECT p.public_key,p.subscription_id,s.rx,s.tx,
			CASE WHEN s.rx>=p.received_counter THEN s.rx-p.received_counter ELSE s.rx END AS rx_delta,
			CASE WHEN s.tx>=p.sent_counter THEN s.tx-p.sent_counter ELSE s.tx END AS tx_delta
		FROM samples s JOIN peers p ON p.public_key=s.key
		WHERE p.node_id=$1 AND s.rx>=0 AND s.tx>=0
	), counters AS (
		UPDATE peers p SET received_counter=d.rx,sent_counter=d.tx FROM deltas d
		WHERE p.public_key=d.public_key AND (p.received_counter,p.sent_counter) IS DISTINCT FROM (d.rx,d.tx)
	), usage AS (
		UPDATE subscriptions s SET traffic_used_bytes=s.traffic_used_bytes+d.bytes FROM (
			SELECT subscription_id,sum(rx_delta+tx_delta)::bigint AS bytes FROM deltas
			GROUP BY subscription_id HAVING sum(rx_delta+tx_delta)>0
		) d WHERE s.id=d.subscription_id RETURNING s.id
	)
	INSERT INTO traffic_daily(day,node_id,user_id,received_bytes,sent_bytes)
	SELECT (now() AT TIME ZONE 'UTC')::date,$1,s.user_id,sum(d.rx_delta),sum(d.tx_delta)
	FROM deltas d JOIN subscriptions s ON s.id=d.subscription_id JOIN usage u ON u.id=s.id GROUP BY s.user_id
	ON CONFLICT(node_id,day,user_id) DO UPDATE
	SET received_bytes=traffic_daily.received_bytes+excluded.received_bytes,
	    sent_bytes=traffic_daily.sent_bytes+excluded.sent_bytes`, nodeID, counters)
	return err
}

type DailyTraffic struct {
	Day      string `json:"day"`
	Received int64  `json:"received_bytes"`
	Sent     int64  `json:"sent_bytes"`
}
type APIToken struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

func ScanAPIToken(row pgx.Row) (APIToken, error) {
	var token APIToken
	err := row.Scan(&token.ID, &token.Name, &token.CreatedAt)
	return token, err
}

type UserQuery struct {
	Page    int
	PerPage int
	Search  string
	Status  string
	Sort    string
}
type UserPage struct {
	Items       []User `json:"items"`
	Total       int64  `json:"total"`
	OnlineTotal int64  `json:"online_total"`
	Page        int    `json:"page"`
	PerPage     int    `json:"per_page"`
	TotalPages  int64  `json:"total_pages"`
}
