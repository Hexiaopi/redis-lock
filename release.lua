-- 获得的value和期望的value是一致的说明是自己的锁
if redis.call("get", KEYS[1]) == ARGV[1] then
  return redis.call("del", KEYS[1])
else
  return 0
end