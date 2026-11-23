package store

import (
	admin "dementia/backend/internal/storage/admin"
	agent "dementia/backend/internal/storage/agent"
	api_tokens "dementia/backend/internal/storage/api_tokens"
	branding "dementia/backend/internal/storage/branding"
	devices "dementia/backend/internal/storage/devices"
	events "dementia/backend/internal/storage/events"
	lifecycle "dementia/backend/internal/storage/lifecycle"
	nodes "dementia/backend/internal/storage/nodes"
	plans "dementia/backend/internal/storage/plans"
	"dementia/backend/internal/storage/shared"
	subscriptions "dementia/backend/internal/storage/subscriptions"
	telegram "dementia/backend/internal/storage/telegram"
	traffic "dementia/backend/internal/storage/traffic"
	users "dementia/backend/internal/storage/users"
	"dementia/backend/internal/vpn"

	"github.com/jackc/pgx/v5/pgxpool"
)

type SubscriptionBranding = shared.SubscriptionBranding

var ErrDeviceLimit = shared.ErrDeviceLimit
var ErrAmbiguousUser = shared.ErrAmbiguousUser

type Device = shared.Device

type DeviceServer = shared.DeviceServer
type DeviceOption = shared.DeviceOption
type ConnectionCheck = shared.ConnectionCheck
type ConnectionChange = shared.ConnectionChange
type Event = shared.Event

var RecordDeletion = shared.RecordDeletion

type SubscriptionPlan = shared.SubscriptionPlan

var ErrPlanInUse = shared.ErrPlanInUse

type TelegramNotification = shared.TelegramNotification
type TelegramNodeMetric = shared.TelegramNodeMetric
type TelegramSettings = shared.TelegramSettings
type User = shared.User

var ErrNodeUnavailable = shared.ErrNodeUnavailable
var ErrAddressPoolFull = shared.ErrAddressPoolFull

type Node = shared.Node
type NodeMetrics = shared.NodeMetrics

func NewToken() string              { return shared.NewToken() }
func HashToken(value string) []byte { return shared.HashToken(value) }

type Subscription = shared.Subscription

type Location = shared.Location

const ClientProfilesQuery = shared.ClientProfilesQuery

type Peer = shared.Peer
type State = shared.State

type TrafficCounter = shared.TrafficCounter

type DailyTraffic = shared.DailyTraffic
type APIToken = shared.APIToken

type AdminRepository = admin.Repository
type AgentRepository = agent.Repository
type APITokensRepository = api_tokens.Repository
type BrandingRepository = branding.Repository
type DevicesRepository = devices.Repository
type EventsRepository = events.Repository
type LifecycleRepository = lifecycle.Repository
type NodesRepository = nodes.Repository
type PlansRepository = plans.Repository
type SubscriptionsRepository = subscriptions.Repository
type TelegramRepository = telegram.Repository
type TrafficRepository = traffic.Repository
type UsersRepository = users.Repository
type Store struct {
	*shared.Connection
	*AdminRepository
	*AgentRepository
	*APITokensRepository
	*BrandingRepository
	*DevicesRepository
	*EventsRepository
	*LifecycleRepository
	*NodesRepository
	*PlansRepository
	*SubscriptionsRepository
	*TelegramRepository
	*TrafficRepository
	*UsersRepository
}

func New(db *pgxpool.Pool, vault vpn.Vault) Store {
	connection := &shared.Connection{DB: db, Vault: vault}
	s := &Store{Connection: connection}
	s.AdminRepository = admin.New(connection, s)
	s.AgentRepository = agent.New(connection, s)
	s.APITokensRepository = api_tokens.New(connection, s)
	s.BrandingRepository = branding.New(connection, s)
	s.DevicesRepository = devices.New(connection, s)
	s.EventsRepository = events.New(connection, s)
	s.LifecycleRepository = lifecycle.New(connection, s)
	s.NodesRepository = nodes.New(connection, s)
	s.PlansRepository = plans.New(connection, s)
	s.SubscriptionsRepository = subscriptions.New(connection, s)
	s.TelegramRepository = telegram.New(connection, s)
	s.TrafficRepository = traffic.New(connection, s)
	s.UsersRepository = users.New(connection, s)
	return *s
}

type UserPage = shared.UserPage
type UserQuery = shared.UserQuery
