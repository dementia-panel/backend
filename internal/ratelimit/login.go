package ratelimit

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

var increment = redis.NewScript(`
local count = redis.call("INCR", KEYS[1])
if count == 1 then redis.call("EXPIRE", KEYS[1], 60) end
return count
`)

type Login struct{ Client *redis.Client }

func (l Login) Allow(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	count, err := increment.Run(ctx, l.Client, []string{"dementia:auth:login"}).Int64()
	return count <= 20, err
}
