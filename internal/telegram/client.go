package telegram

import (
	"bytes"
	"context"
	"dementia/backend/internal/infrastructure/config"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Config = config.Telegram

func ParseConfig(token, mode, chatID, threadID, secret string) (Config, error) {
	return config.ParseTelegram(token, mode, chatID, threadID, secret)
}

type apiError struct {
	code       int
	retryAfter time.Duration
}

func (e *apiError) Error() string {
	return fmt.Sprintf("Telegram API error %d", e.code)
}

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
