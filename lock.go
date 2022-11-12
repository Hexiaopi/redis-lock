package redislock

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/go-redis/redis/v9"
	"github.com/google/uuid"
)

var (
	ErrObtainLockFail  = errors.New("fail to obtain lock")
	ErrReleaseLockFail = errors.New("fail to release lock")
)

var (
	//go:embed release.lua
	releaseLua string
)

type Client struct {
	client redis.Cmdable
}

func NewClient(client redis.Cmdable) *Client {
	return &Client{
		client: client,
	}
}

type lock struct {
	client redis.Cmdable
	key    string
	value  string
}

func (c *Client) ObtainLock(ctx context.Context, key string, expiration time.Duration) (*lock, error) {
	value := uuid.New().String()
	res, err := c.client.SetNX(ctx, key, value, expiration).Result()
	if err != nil {
		return nil, err
	}
	if !res {
		return nil, ErrObtainLockFail
	}
	return newLock(c.client, key, value), nil
}

func newLock(client redis.Cmdable, key, value string) *lock {
	return &lock{
		client: client,
		key:    key,
		value:  value,
	}
}

func (c *lock) Release(ctx context.Context) error {
	res, err := c.client.Eval(ctx, releaseLua, []string{c.key}, c.value).Int64()
	if err == redis.Nil {
		return ErrReleaseLockFail
	}
	if err != nil {
		return err
	}
	if res == 0 {
		return ErrReleaseLockFail
	}
	return nil
}
