package redislock

//go:generate mockgen -package=redislock -destination=mock_lock.go github.com/go-redis/redis/v9 Cmdable
