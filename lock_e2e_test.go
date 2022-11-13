//go:build e2e

package redislock

import (
	"context"
	"testing"
	"time"

	"github.com/go-redis/redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_ObtainLock(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})
	client := NewClient(rdb)
	testCases := []struct {
		name       string
		key        string
		expiration time.Duration
		before     func()
		after      func()
		wantErr    error
		want       *lock
	}{
		{
			name:       "正常获取锁",
			key:        "test-lock-key",
			expiration: time.Minute,
			before:     func() {},
			after: func() {
				res, err := rdb.Del(context.Background(), "test-lock-key").Result()
				require.NoError(t, err)
				require.Equal(t, int64(1), res)
			},
			want: &lock{
				key: "test-lock-key",
			},
		},
		{
			name:       "别人已经获取锁",
			key:        "test-lock-key-exists",
			expiration: time.Minute,
			before: func() { //模拟别人已经获取锁
				res, err := rdb.Set(context.Background(), "test-lock-key-exists", "123", time.Minute).Result()
				require.NoError(t, err)
				require.Equal(t, "OK", res)
			},
			after: func() { //确认是否改了别人的锁
				res, err := rdb.Get(context.Background(), "test-lock-key-exists").Result()
				require.NoError(t, err)
				require.Equal(t, "123", res)
			},
			wantErr: ErrObtainLockFail,
			want:    nil,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.before()
			lock, err := client.ObtainLock(context.Background(), tc.key, tc.expiration)
			assert.Equal(t, tc.wantErr, err)
			if err != nil {
				return
			}
			tc.after()
			assert.NotNil(t, lock)
			assert.Equal(t, tc.want.key, lock.key)
		})
	}
}

func TestLock_Release(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})
	client := NewClient(rdb)
	lock, err := client.ObtainLock(context.Background(), "test-release-lock-key", time.Second*10)
	require.NoError(t, err)
	require.NotNil(t, lock)
	testCases := []struct {
		name    string
		before  func()
		after   func()
		wantErr error
	}{
		{
			name:    "正常删除",
			before:  func() {},
			after:   func() {},
			wantErr: nil,
		},
		{
			name: "锁已被他人获取",
			before: func() { //模拟别人已经获取锁
				res, err := rdb.Set(context.Background(), "test-release-lock-key", "123", time.Minute).Result()
				require.NoError(t, err)
				require.Equal(t, "OK", res)
			},
			after: func() { //确认是否改了别人的锁
				res, err := rdb.Get(context.Background(), "test-release-lock-key").Result()
				require.NoError(t, err)
				require.Equal(t, "123", res)
			},
			wantErr: ErrReleaseLockFail,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.before()
			err := lock.Release(context.Background())
			assert.Equal(t, tc.wantErr, err)
		})
	}
}
