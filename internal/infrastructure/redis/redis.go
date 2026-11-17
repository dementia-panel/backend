package redis

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

func New(ctx context.Context, raw string) (*redis.Client, error) {
	options, err := redis.ParseURL(raw)
	if err != nil {
		return nil, errors.New("invalid REDIS_URL")
	}
	options.ContextTimeoutEnabled = true
	options.MaxRetries = -1
	cache := redis.NewClient(options)
	cacheCtx, cancelCache := context.WithTimeout(ctx, 3*time.Second)
	err = cache.Ping(cacheCtx).Err()
	cancelCache()
	if err != nil {
		_ = cache.Close()
		return nil, errors.New("could not connect to Redis")
	}
	return cache, nil
}
