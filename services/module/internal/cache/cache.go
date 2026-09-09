package cache

import (
	"context"
	"encoding/json"
	"time"

	"github.com/almuzky/iot/services/module/internal/model"
	"github.com/redis/go-redis/v9"
)

// StatusCache stores realtime node connectivity in Redis for fast reads.
type StatusCache struct {
	rdb *redis.Client
}

func New(addr, password string, db int) *StatusCache {
	return &StatusCache{
		rdb: redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: password,
			DB:       db,
		}),
	}
}

func (c *StatusCache) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

func (c *StatusCache) Close() error {
	return c.rdb.Close()
}

// SetStatus caches a node's status + last-seen. TTL keeps the "online" view fresh:
// if a node stops reporting, the entry expires and it is treated as stale/offline.
func (c *StatusCache) SetStatus(ctx context.Context, nodeID, status string, ttl time.Duration) {
	if c == nil || c.rdb == nil {
		return
	}
	key := "node:status:" + nodeID
	_ = c.rdb.HSet(ctx, key,
		"status", status,
		"last_seen", time.Now().UTC().Format(time.RFC3339),
	).Err()
	if ttl > 0 {
		_ = c.rdb.Expire(ctx, key, ttl).Err()
	}
}

// GetStatus returns the cached status for a node ("" if absent).
func (c *StatusCache) GetStatus(ctx context.Context, nodeID string) string {
	if c == nil || c.rdb == nil {
		return ""
	}
	return c.rdb.HGet(ctx, "node:status:"+nodeID, "status").Val()
}

// SetLatest stores the most recent raw telemetry payload for a node (TTL).
func (c *StatusCache) SetLatest(ctx context.Context, nodeID string, raw []byte, ttl time.Duration) {
	if c == nil || c.rdb == nil {
		return
	}
	_ = c.rdb.Set(ctx, "node:latest:"+nodeID, raw, ttl).Err()
}

// GetLatest returns the most recent raw telemetry payload for a node (nil if absent).
func (c *StatusCache) GetLatest(ctx context.Context, nodeID string) []byte {
	if c == nil || c.rdb == nil {
		return nil
	}
	res := c.rdb.Get(ctx, "node:latest:"+nodeID)
	b, err := res.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// GetCachedModules returns cached modules if present.
func (c *StatusCache) GetCachedModules(ctx context.Context) ([]model.Module, bool) {
	if c == nil || c.rdb == nil {
		return nil, false
	}
	val, err := c.rdb.Get(ctx, "cache:modules:all").Result()
	if err != nil {
		return nil, false
	}
	var modules []model.Module
	if err := json.Unmarshal([]byte(val), &modules); err != nil {
		return nil, false
	}
	return modules, true
}

// SetCachedModules caches the module list in Redis.
func (c *StatusCache) SetCachedModules(ctx context.Context, modules []model.Module, ttl time.Duration) {
	if c == nil || c.rdb == nil {
		return
	}
	b, err := json.Marshal(modules)
	if err != nil {
		return
	}
	_ = c.rdb.Set(ctx, "cache:modules:all", b, ttl).Err()
}

// InvalidateModules clears module list cache in Redis.
func (c *StatusCache) InvalidateModules(ctx context.Context) {
	if c == nil || c.rdb == nil {
		return
	}
	_ = c.rdb.Del(ctx, "cache:modules:all").Err()
}
