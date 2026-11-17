package application

import (
	"dementia/backend/internal/api"
	"dementia/backend/internal/infrastructure/config"
	"dementia/backend/internal/integration"
	"dementia/backend/internal/store"
	"dementia/backend/internal/telegram"
)

func initializeTelegram(app *api.API, c *config.Config, repo store.Store, status *integration.Tracker) *telegram.Bot {
	var bot *telegram.Bot
	if c.Bot.Token != "" {
		bot = telegram.New(c.Bot, repo)
		if bot.WebhookEnabled() {
			app.TelegramWebhook = bot.Webhook
		}
	}
	app.IntegrationStatus = func() integration.Snapshot {
		telegramStatus := integration.NewTracker(false).Snapshot()
		if bot != nil {
			telegramStatus = bot.Status()
		}
		return integration.Snapshot{Telegram: telegramStatus, Webhook: status.Snapshot()}
	}
	return bot
}
