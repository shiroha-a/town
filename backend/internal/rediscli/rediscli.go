// Package rediscli provides the Redis client. Redis is used only for ephemeral
// state (sessions, locks, pub/sub, cache, rate limits); it is never the source
// of truth for game state.
package rediscli

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Connect creates a Redis client and verifies connectivity.
func Connect(ctx context.Context, addr string, db int) (*redis.Client, error) {
	c := redis.NewClient(&redis.Options{Addr: addr, DB: db})
	if err := c.Ping(ctx).Err(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return c, nil
}
