package redislock

import (
	"context"
	"errors"
	"time"

	"github.com/go-redis/redis/v9"
	"github.com/google/uuid"
)

var (
	ErrObtainLockFail  = errors.New("fail to obtain lock")
	ErrReleaseLockFail = errors.New("fail to release lock")
)

type Client struct {
	client redis.Cmdable
}

func NewClient(client redis.Cmdable) *Client {
	return &Client{
		client: client,
	}
}

type Lock struct {
	client redis.Cmdable
	key    string
}

func (c *Client) ObtainLock(ctx context.Context, key string, expiration time.Duration) (*Lock, error) {
	value := uuid.New().String()
	res, err := c.client.SetNX(ctx, key, value, expiration).Result()
	if err != nil {
		return nil, err
	}
	if !res {
		return nil, ErrObtainLockFail
	}
	return newLock(c.client, key), nil
}

func newLock(client redis.Cmdable, key string) *Lock {
	return &Lock{
		client: client,
		key:    key,
	}
}

func (c *Lock) Release(ctx context.Context) error {
	res, err := c.client.Del(ctx, c.key).Result()
	if err != nil {
		return err
	}
	if res != 1 {
		return ErrReleaseLockFail
	}
	return nil
}
