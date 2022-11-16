--续约分布式锁
if redis.call("get", KEYS[1]) == ARGV[1] then
  return redis.call("pexpire", KEYS[1], ARGV[2]) --返回1成功，返回0失败
else
  return 0
end