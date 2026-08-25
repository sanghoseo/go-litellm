package redis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	redisclient "github.com/redis/go-redis/v9"
)

var ErrCacheMiss = errors.New("cache value not found")

type Client struct {
	client *redisclient.Client
}

func New(address string) (*Client, error) {
	if address == "" {
		return nil, fmt.Errorf("Redis address is required")
	}
	options, err := redisOptions(address)
	if err != nil {
		return nil, err
	}
	return &Client{client: redisclient.NewClient(options)}, nil
}

func (client *Client) Close() error {
	if client == nil || client.client == nil {
		return nil
	}
	return client.client.Close()
}

func (client *Client) Ping(ctx context.Context) error {
	if client == nil || client.client == nil {
		return fmt.Errorf("Redis client is not configured")
	}
	return client.client.Ping(ctx).Err()
}

func (client *Client) Get(ctx context.Context, key string) ([]byte, error) {
	value, err := client.client.Get(ctx, key).Bytes()
	if errors.Is(err, redisclient.Nil) {
		return nil, ErrCacheMiss
	}
	return value, err
}

func (client *Client) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return client.client.Set(ctx, key, value, ttl).Err()
}

func (client *Client) Delete(ctx context.Context, key string) error {
	return client.client.Del(ctx, key).Err()
}

func (client *Client) Allow(ctx context.Context, key string, limit int64, window time.Duration) (bool, error) {
	if limit <= 0 || window <= 0 {
		return false, fmt.Errorf("rate limit and window must be positive")
	}
	count, err := client.client.Incr(ctx, key).Result()
	if err != nil {
		return false, err
	}
	if count == 1 {
		if err := client.client.Expire(ctx, key, window).Err(); err != nil {
			return false, err
		}
	}
	return count <= limit, nil
}

func (client *Client) AcquireLock(ctx context.Context, key string, token string, ttl time.Duration) (bool, error) {
	if token == "" || ttl <= 0 {
		return false, fmt.Errorf("lock token and ttl are required")
	}
	return client.client.SetNX(ctx, key, token, ttl).Result()
}

func (client *Client) ReleaseLock(ctx context.Context, key string, token string) (bool, error) {
	const releaseLock = `if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`
	result, err := client.client.Eval(ctx, releaseLock, []string{key}, token).Int64()
	return result == 1, err
}

func redisOptions(address string) (*redisclient.Options, error) {
	if strings.Contains(address, "://") {
		options, err := redisclient.ParseURL(address)
		if err != nil {
			return nil, fmt.Errorf("parse Redis URL: %w", err)
		}
		return options, nil
	}
	return &redisclient.Options{Addr: address}, nil
}
