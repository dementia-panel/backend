package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

type Config struct {
	Token         string
	Mode          string
	ChatID        int64
	ThreadID      int64
	WebhookSecret string
}

var tokenPattern = regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]+$`)
var secretPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)

func ParseConfig(token, mode, chatID, threadID, secret string) (Config, error) {
	if token == "" {
		return Config{}, nil
	}
	if !tokenPattern.MatchString(token) {
		return Config{}, errors.New("invalid TELEGRAM_BOT_TOKEN")
	}
	if mode == "" {
		mode = "polling"
	}
	if mode != "polling" && mode != "webhook" {
		return Config{}, errors.New("TELEGRAM_MODE must be polling or webhook")
	}
	chat, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil || chat == 0 {
		return Config{}, errors.New("TELEGRAM_CHAT_ID must be a nonzero integer")
	}
	var thread int64
	if threadID != "" {
		thread, err = strconv.ParseInt(threadID, 10, 32)
		if err != nil || thread < 1 {
			return Config{}, errors.New("TELEGRAM_THREAD_ID must be positive")
		}
	}
	if mode == "webhook" && (!secretPattern.MatchString(secret) || len(secret) < 32) {
		return Config{}, errors.New("TELEGRAM_WEBHOOK_SECRET must contain 32–256 letters, digits, underscores or hyphens")
	}
	return Config{token, mode, chat, thread, secret}, nil
}

type apiError struct {
	code       int
	retryAfter time.Duration
}

func (e *apiError) Error() string { return fmt.Sprintf("Telegram API error %d", e.code) }

type client struct {
	baseURL string
	http    *http.Client
}

func (c client) call(ctx context.Context, method string, payload, result any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/"+method, bytes.NewReader(data))
	if err != nil {
		return errors.New("invalid Telegram request")
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return errors.New("Telegram request failed")
	}
	defer response.Body.Close()
	var envelope struct {
		OK         bool            `json:"ok"`
		Result     json.RawMessage `json:"result"`
		Code       int             `json:"error_code"`
		Parameters struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&envelope); err != nil {
		return &apiError{code: response.StatusCode}
	}
	if !envelope.OK || response.StatusCode != http.StatusOK {
		if envelope.Code == 0 {
			envelope.Code = response.StatusCode
		}
		return &apiError{code: envelope.Code, retryAfter: time.Duration(max(0, min(envelope.Parameters.RetryAfter, 86400))) * time.Second}
	}
	if result != nil {
		return json.Unmarshal(envelope.Result, result)
	}
	return nil
}
