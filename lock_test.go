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
	"github.com/stretchr/testify/require"
)

func TestClient_SingleFlightLock(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	rdb := NewMockCmdable(ctrl)
	cmd := redis.NewCmd(context.Background())
	cmd.SetVal("OK")
	rdb.EXPECT().Eval(gomock.Any(), lockLua, gomock.Any(), gomock.Any()).Return(cmd)
	client := NewClient(rdb)
	_, err := client.SingleFlightLock(context.Background(), "singleflight-key", time.Minute, LimitedRetry(time.Second*30, 3))
	require.NoError(t, err)
}

func TestClient_Lock(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	type fields struct {
		client redis.Cmdable
	}
	type args struct {
		timeout    time.Duration
		key        string
		expiration time.Duration
		try        RetryStrategy
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
				cmd := redis.NewCmd(context.Background())
				cmd.SetVal("OK")
				rdb.EXPECT().Eval(gomock.Any(), lockLua, []string{"locked-key"}, gomock.Any()).Return(cmd)
				return rdb
			}()},
			args:    args{time.Minute, "locked-key", time.Second, LimitedRetry(time.Millisecond*100, 3)},
			want:    &lock{key: "locked-key", expiration: time.Second},
			wantErr: nil,
		},
		{
			name: "网络错误",
			fields: fields{client: func() redis.Cmdable {
				rdb := NewMockCmdable(ctrl)
				cmd := redis.NewCmd(context.Background())
				cmd.SetErr(errors.New("network error"))
				rdb.EXPECT().Eval(gomock.Any(), lockLua, []string{"locked-key"}, gomock.Any()).Return(cmd)
				return rdb
			}()},
			args:    args{time.Minute, "locked-key", time.Second, LimitedRetry(time.Millisecond*100, 3)},
			wantErr: errors.New("network error"),
		},
		{
			name: "网络超时错误",
			fields: fields{client: func() redis.Cmdable {
				rdb := NewMockCmdable(ctrl)
				cmd := redis.NewCmd(context.Background())
				cmd.SetErr(context.DeadlineExceeded)
				rdb.EXPECT().Eval(gomock.Any(), lockLua, []string{"locked-key"}, gomock.Any()).Times(3).Return(cmd) //执行三次
				return rdb
			}()},
			args:    args{time.Minute, "locked-key", time.Second, LimitedRetry(time.Millisecond*100, 3)},
			wantErr: context.DeadlineExceeded,
		},
		{
			name: "整体超时错误",
			fields: fields{client: func() redis.Cmdable {
				rdb := NewMockCmdable(ctrl)
				cmd := redis.NewCmd(context.Background())
				cmd.SetErr(context.DeadlineExceeded)
				rdb.EXPECT().Eval(gomock.Any(), lockLua, []string{"locked-key"}, gomock.Any()).Times(3).Return(cmd) //执行3次
				return rdb
			}()},
			args:    args{time.Second, "locked-key", time.Second, LimitedRetry(time.Millisecond*400, 3)}, //重试2次后超时
			wantErr: context.DeadlineExceeded,
		},
		{
			name: "不重试错误",
			fields: fields{client: func() redis.Cmdable {
				rdb := NewMockCmdable(ctrl)
				cmd := redis.NewCmd(context.Background())
				cmd.SetErr(context.DeadlineExceeded)
				rdb.EXPECT().Eval(gomock.Any(), lockLua, []string{"locked-key"}, gomock.Any()).Return(cmd) //执行1次
				return rdb
			}()},
			args:    args{time.Second, "locked-key", time.Second, NoRetry()},
			wantErr: context.DeadlineExceeded,
		},
		{
			name: "重试三次成功",
			fields: fields{client: func() redis.Cmdable {
				rdb := NewMockCmdable(ctrl)
				first := redis.NewCmd(context.Background())
				first.SetVal("")
				rdb.EXPECT().Eval(gomock.Any(), lockLua, []string{"retry-key"}, gomock.Any()).Times(2).Return(first) //前两次拿不到
				cmd := redis.NewCmd(context.Background())
				cmd.SetVal("OK")
				rdb.EXPECT().Eval(gomock.Any(), lockLua, []string{"retry-key"}, gomock.Any()).Return(cmd) //第三次拿到
				return rdb
			}()},
			args:    args{time.Minute, "retry-key", time.Second, LimitedRetry(time.Millisecond*100, 3)},
			want:    &lock{key: "retry-key", expiration: time.Second},
			wantErr: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{
				client: tt.fields.client,
			}
			ctx, cancel := context.WithTimeout(context.Background(), tt.args.timeout)
			defer cancel()
			got, err := c.Lock(ctx, tt.args.key, tt.args.expiration, tt.args.try)
			assert.Equal(t, tt.wantErr, err)
			if err != nil {
				return
			}
			assert.NotNil(t, got)
			assert.Equal(t, tt.want.key, got.key)
			assert.Equal(t, tt.want.expiration, got.expiration)
			assert.NotEmpty(t, got.value)
		})
	}
}

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
			c := newLock(tt.fields.client, "test-failed-key", "test-failed-value", time.Minute)
			err := c.Release(tt.args.ctx)
			assert.Equal(t, tt.wantErr, err)
		})
	}
}

func Test_lock_Refresh(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	type fields struct {
		client     redis.Cmdable
		key        string
		value      string
		expiration time.Duration
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
			name: "续约成功",
			fields: fields{
				client: func() redis.Cmdable {
					rdb := NewMockCmdable(ctrl)
					res := redis.NewCmd(context.Background())
					res.SetVal(int64(1))
					rdb.EXPECT().Eval(gomock.Any(), refreshLua, []string{"refresh-key"}, []any{"refresh-value", int64(1000)}).Return(res)
					return rdb
				}(),
				key:        "refresh-key",
				value:      "refresh-value",
				expiration: time.Second,
			},
			args:    args{ctx: context.Background()},
			wantErr: nil,
		},
		{
			name: "续约失败",
			fields: fields{
				client: func() redis.Cmdable {
					rdb := NewMockCmdable(ctrl)
					res := redis.NewCmd(context.Background())
					res.SetVal(int64(0))
					rdb.EXPECT().Eval(gomock.Any(), refreshLua, []string{"refresh-key"}, []any{"refresh-value", int64(1000)}).Return(res)
					return rdb
				}(),
				key:        "refresh-key",
				value:      "refresh-value",
				expiration: time.Second,
			},
			args:    args{ctx: context.Background()},
			wantErr: ErrRefreshLockFail,
		},
		{
			name: "网络失败",
			fields: fields{
				client: func() redis.Cmdable {
					rdb := NewMockCmdable(ctrl)
					res := redis.NewCmd(context.Background())
					res.SetErr(errors.New("network error"))
					rdb.EXPECT().Eval(gomock.Any(), refreshLua, []string{"refresh-key"}, []any{"refresh-value", int64(1000)}).Return(res)
					return rdb
				}(),
				key:        "refresh-key",
				value:      "refresh-value",
				expiration: time.Second,
			},
			args:    args{ctx: context.Background()},
			wantErr: errors.New("network error"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lock := newLock(tt.fields.client, tt.fields.key, tt.fields.value, tt.fields.expiration)
			err := lock.Refresh(tt.args.ctx)
			assert.Equal(t, tt.wantErr, err)
		})
	}
}

func Test_lock_AutoRefresh(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	type fields struct {
		client     redis.Cmdable
		key        string
		value      string
		expiration time.Duration
	}
	type args struct {
		interval time.Duration
		timeout  time.Duration
	}
	tests := []struct {
		name        string
		fields      fields
		args        args
		releaseTime time.Duration
		wantErr     error
	}{
		{
			name: "自动续约成功",
			fields: fields{
				client: func() redis.Cmdable {
					rdb := NewMockCmdable(ctrl)
					res := redis.NewCmd(context.Background())
					res.SetVal(int64(1))
					rdb.EXPECT().Eval(gomock.Any(), refreshLua, []string{"auto-refresh-key"}, []any{"auto-refresh-value", int64(60000)}).
						AnyTimes().Return(res)
					cmd := redis.NewCmd(context.Background())
					cmd.SetVal(int64(1))
					rdb.EXPECT().Eval(gomock.Any(), releaseLua, gomock.Any(), gomock.Any()).Return(cmd)
					return rdb
				}(),
				key:        "auto-refresh-key",
				value:      "auto-refresh-value",
				expiration: time.Minute,
			},
			args:        args{interval: time.Second, timeout: time.Millisecond * 10},
			releaseTime: time.Second * 2,
			wantErr:     nil,
		},
		{
			name: "网络错误",
			fields: fields{
				client: func() redis.Cmdable {
					rdb := NewMockCmdable(ctrl)
					res := redis.NewCmd(context.Background())
					res.SetErr(errors.New("network error"))
					rdb.EXPECT().Eval(gomock.Any(), refreshLua, []string{"auto-refresh-key"}, []any{"auto-refresh-value", int64(60000)}).
						AnyTimes().Return(res)
					cmd := redis.NewCmd(context.Background())
					cmd.SetVal(int64(1))
					rdb.EXPECT().Eval(gomock.Any(), releaseLua, gomock.Any(), gomock.Any()).AnyTimes().Return(cmd)
					return rdb
				}(),
				key:        "auto-refresh-key",
				value:      "auto-refresh-value",
				expiration: time.Minute,
			},
			args:        args{interval: time.Second, timeout: time.Millisecond},
			releaseTime: time.Second * 2,
			wantErr:     errors.New("network error"),
		},
		{
			name: "续约超时",
			fields: fields{
				client: func() redis.Cmdable {
					rdb := NewMockCmdable(ctrl)
					first := redis.NewCmd(context.Background())
					first.SetErr(context.DeadlineExceeded)
					rdb.EXPECT().Eval(gomock.Any(), refreshLua, []string{"auto-refresh-key"}, []any{"auto-refresh-value", int64(60000)}).AnyTimes().
						Return(first)
					cmd := redis.NewCmd(context.Background())
					cmd.SetVal(int64(1))
					rdb.EXPECT().Eval(gomock.Any(), releaseLua, gomock.Any(), gomock.Any()).Return(cmd)
					return rdb
				}(),
				key:        "auto-refresh-key",
				value:      "auto-refresh-value",
				expiration: time.Minute,
			},
			args:        args{interval: time.Second, timeout: time.Millisecond},
			releaseTime: time.Second * 2,
			wantErr:     nil,
		},
	}
	for _, tt := range tests {
		tc := tt
		t.Run(tc.name, func(t *testing.T) {
			lock := newLock(tc.fields.client, tc.fields.key, tc.fields.value, tc.fields.expiration)
			go func() {
				time.Sleep(tc.releaseTime)
				err := lock.Release(context.Background())
				require.NoError(t, err)
			}()
			err := lock.AutoRefresh(tc.args.interval, tc.args.timeout)
			assert.Equal(t, tc.wantErr, err)
		})
	}
}
