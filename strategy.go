package redislock

import "time"

type RetryStrategy interface {
	//返回下一次是否需要重试以及重试间隔
	Next() (bool, time.Duration)
}

type limitedRetry struct {
	interval time.Duration
	max      int64
	cnt      int64
}

func (retry *limitedRetry) Next() (bool, time.Duration) {
	retry.cnt++
	return retry.cnt < retry.max, retry.interval
}

func LimitedRetry(interval time.Duration, max int64) RetryStrategy {
	return &limitedRetry{interval: interval, max: max, cnt: 0}
}

type noRetry struct{}

func (retry *noRetry) Next() (bool, time.Duration) {
	return false, 0
}

func NoRetry() RetryStrategy {
	return &noRetry{}
}
