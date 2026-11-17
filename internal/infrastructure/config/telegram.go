package config

import (
	"errors"
	"regexp"
	"strconv"
)

type Telegram struct {
	Token         string
	Mode          string
	ChatID        int64
	ThreadID      int64
	WebhookSecret string
}

var tokenPattern = regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]+$`)
var secretPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)

func ParseTelegram(token, mode, chatID, threadID, secret string) (Telegram, error) {
	if token == "" {
		return Telegram{}, nil
	}
	if !tokenPattern.MatchString(token) {
		return Telegram{}, errors.New("invalid TELEGRAM_BOT_TOKEN")
	}
	if mode == "" {
		mode = "polling"
	}
	if mode != "polling" && mode != "webhook" {
		return Telegram{}, errors.New("TELEGRAM_MODE must be polling or webhook")
	}
	chat, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil || chat == 0 {
		return Telegram{}, errors.New("TELEGRAM_CHAT_ID must be a nonzero integer")
	}
	var thread int64
	if threadID != "" {
		thread, err = strconv.ParseInt(threadID, 10, 32)
		if err != nil || thread < 1 {
			return Telegram{}, errors.New("TELEGRAM_THREAD_ID must be positive")
		}
	}
	if mode == "webhook" && (!secretPattern.MatchString(secret) || len(secret) < 32) {
		return Telegram{}, errors.New("TELEGRAM_WEBHOOK_SECRET must contain 32–256 letters, digits, underscores or hyphens")
	}
	return Telegram{token, mode, chat, thread, secret}, nil
}
