#!lua name=get_total_balance
-- FCALL get_total_balance
local function get_total_balance(keys, args)
  local total_balance = 0
  for _, keyName in ipairs(keys) do
    local balance = redis.call('GET', keyName)
    total_balance = total_balance + balance
  end
  return total_balance
end
redis.register_function(
  'get_total_balance',
  get_total_balance
)

