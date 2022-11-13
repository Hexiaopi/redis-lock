package redislock

import (
	"context"
	_ "embed"
	"errors"
	"testing"
	"time"

	"github.com/go-redis/redis/v9"
	gomock "github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func TestClient_ObtainLock(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	type fields struct {
		client redis.Cmdable
	}
	type args struct {
		ctx        context.Context
		key        string
		expiration time.Duration
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    *lock
		wantErr error
	}{
		{
			name: "正常测试",
			fields: fields{client: func() redis.Cmdable {
				rdb := NewMockCmdable(ctrl)
				res := redis.NewBoolResult(true, nil)
				rdb.EXPECT().SetNX(gomock.Any(), "test-locked-key", gomock.Any(), time.Minute).Return(res)
				return rdb
			}()},
			args:    args{context.Background(), "test-locked-key", time.Minute},
			want:    &lock{key: "test-locked-key"},
			wantErr: nil,
		},
		{
			name: "网络错误",
			fields: fields{client: func() redis.Cmdable {
				rdb := NewMockCmdable(ctrl)
				res := redis.NewBoolResult(false, errors.New("network error"))
				rdb.EXPECT().SetNX(gomock.Any(), "network-key", gomock.Any(), time.Minute).Return(res)
				return rdb
			}()},
			args:    args{context.Background(), "network-key", time.Minute},
			want:    nil,
			wantErr: errors.New("network error"),
		},
		{
			name: "加锁失败",
			fields: fields{client: func() redis.Cmdable {
				rdb := NewMockCmdable(ctrl)
				res := redis.NewBoolResult(false, nil)
				rdb.EXPECT().SetNX(gomock.Any(), "test-lock-key", gomock.Any(), time.Minute).Return(res)
				return rdb
			}()},
			args:    args{context.Background(), "test-lock-key", time.Minute},
			want:    nil,
			wantErr: ErrObtainLockFail,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{
				client: tt.fields.client,
			}
			got, err := c.ObtainLock(tt.args.ctx, tt.args.key, tt.args.expiration)
			assert.Equal(t, tt.wantErr, err)
			if err != nil {
				return
			}
			assert.NotNil(t, got)
			assert.Equal(t, tt.want.key, got.key)
		})
	}
}

func Test_lock_Release(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	type fields struct {
		client redis.Cmdable
	}
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr error
	}{
		{
			name: "正常测试",
			fields: fields{
				client: func() redis.Cmdable {
					rdb := NewMockCmdable(ctrl)
					cmd := redis.NewCmd(context.Background())
					cmd.SetVal(int64(1))
					rdb.EXPECT().Eval(gomock.Any(), releaseLua, gomock.Any(), gomock.Any()).Return(cmd)
					return rdb
				}(),
			},
			args:    args{context.Background()},
			wantErr: nil,
		},
		{
			name: "网络错误",
			fields: fields{
				client: func() redis.Cmdable {
					rdb := NewMockCmdable(ctrl)
					cmd := redis.NewCmd(context.Background())
					cmd.SetErr(errors.New("network error"))
					rdb.EXPECT().Eval(gomock.Any(), releaseLua, gomock.Any(), gomock.Any()).Return(cmd)
					return rdb
				}(),
			},
			args:    args{context.Background()},
			wantErr: errors.New("network error"),
		},
		{
			name: "释放锁失败",
			fields: fields{
				client: func() redis.Cmdable {
					rdb := NewMockCmdable(ctrl)
					cmd := redis.NewCmd(context.Background())
					cmd.SetVal(int64(0))
					rdb.EXPECT().Eval(gomock.Any(), releaseLua, gomock.Any(), gomock.Any()).Return(cmd)
					return rdb
				}(),
			},
			args:    args{context.Background()},
			wantErr: ErrReleaseLockFail,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newLock(tt.fields.client, "test-failed-key", "test-failed-value")
			err := c.Release(tt.args.ctx)
			assert.Equal(t, tt.wantErr, err)
		})
	}
}
