package telegram

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"dementia/backend/internal/store"
)

func (b *Bot) DeliverNotifications(ctx context.Context) {
	for ctx.Err() == nil {
		queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		events, err := b.repo.PendingTelegram(queryCtx)
		cancel()
		if err != nil && ctx.Err() == nil {
			slog.Error("could not read Telegram notification queue")
		}
		for _, event := range events {
			if ctx.Err() != nil {
				return
			}
			err := b.send(ctx, formatNotification(event))
			if err != nil && ctx.Err() != nil {
				return
			}
			updateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			if err == nil {
				err = b.repo.CompleteTelegram(updateCtx, event.ID)
			} else {
				delay := retryDelay(err, min(time.Second<<min(event.Attempts, 10), 15*time.Minute))
				slog.Warn("Telegram notification failed", "event_id", event.ID, "error", err, "retry_in", delay)
				err = b.repo.RetryTelegram(updateCtx, event.ID, delay)
				if err == nil {
					cancel()
					pause(ctx, min(delay, time.Minute))
					break
				}
			}
			cancel()
			if err != nil {
				slog.Error("could not update Telegram notification", "event_id", event.ID)
			}
		}
		pause(ctx, time.Second)
	}
}

var labels = map[string]string{
	"node.created":                "Нода добавлена",
	"node.modified":               "Нода изменена",
	"node.enabled":                "Нода включена",
	"node.disabled":               "Нода на паузе",
	"node.deleted":                "Нода выведена из эксплуатации",
	"node.installation_started":   "Установка ноды началась",
	"node.installation_completed": "Нода установлена",
	"node.installation_failed":    "Ошибка установки ноды",
	"node.connection_lost":        "Связь с нодой потеряна",
	"node.connection_restored":    "Связь с нодой восстановлена",
	"node.restart_requested":      "Перезапуск ноды запрошен",
	"node.restarted":              "Нода перезапущена",
}

func formatNotification(event store.TelegramNotification) string {
	var data struct {
		Name  string `json:"name"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(event.Data, &data)
	title := labels[event.Event]
	if title == "" {
		title = event.Event
	}
	if data.Name != "" {
		title += " · " + data.Name
	}
	if data.Error != "" {
		title += "\n" + data.Error
	}
	return title
}
