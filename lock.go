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
	ErrRefreshLockFail = errors.New("fail to refresh lock")
)

var (
	//go:embed release.lua
	releaseLua string
	//go:embed refresh.lua
	refreshLua string
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
	client     redis.Cmdable
	key        string
	value      string
	expiration time.Duration
	release    chan struct{}
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
	return newLock(c.client, key, value, expiration), nil
}

func newLock(client redis.Cmdable, key, value string, expiration time.Duration) *lock {
	return &lock{
		client:     client,
		key:        key,
		value:      value,
		expiration: expiration,
		release:    make(chan struct{}),
	}
}

func (c *lock) AutoRefresh(interval, timeout time.Duration) error {
	if interval >= c.expiration {
		return errors.New("interval should be less than expiration")
	}
	ticker := time.NewTicker(interval)
	ch := make(chan struct{}, 1)
	defer func() {
		ticker.Stop()
		close(ch)
	}()
	for {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			err := c.Refresh(ctx)
			cancel()
			if err == context.DeadlineExceeded {
				select {
				case ch <- struct{}{}:
				default:
				}
				continue
			}
			if err != nil {
				return err
			}
		case <-ch:
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			err := c.Refresh(ctx)
			cancel()
			if err == context.DeadlineExceeded {
				select {
				case ch <- struct{}{}:
				default:
				}
				continue
			}
			if err != nil {
				return err
			}
		case <-c.release: //主动释放锁场景
			return nil
		}
	}
}

func (c *lock) Refresh(ctx context.Context) error {
	res, err := c.client.Eval(ctx, refreshLua, []string{c.key}, c.value, c.expiration.Milliseconds()).Int64()
	if err == redis.Nil {
		return ErrRefreshLockFail
	}
	if err != nil {
		return err
	}
	if res != 1 {
		return ErrRefreshLockFail
	}
	return nil
}

func (c *lock) Release(ctx context.Context) error {
	defer func() {
		close(c.release) //告诉AutoRefresh不必继续续约
	}()
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
