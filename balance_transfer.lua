#!lua name=balance_transfer
-- FCALL balance transfer 3 balance:mandiri balance:bca fee:mandiri 10 2
local function balance_transfer(keys, args)
  local source_account = keys[1]
  local target_account = keys[2]
  local fee_account = keys[3]
  local amount = tonumber(args[1])
  local fee = tonumber(args[2])

  -- check if source account has sufficient balance
  local source_balance = redis.call('GET', source_account)
  if(tonumber(source_balance) < (amount+fee)) then 
    return tonumber(-1) -- insufficient balance ERROR CODE
  end
    
  redis.call('INCRBY', source_account, -1 * (amount+fee))
  redis.call('INCRBY', fee_account, fee)
  redis.call('INCRBY', target_account, amount)
  -- redis.call('WAITAOF', 1)
  return amount+fee
end
redis.register_function(
  'balance_transfer',
  balance_transfer
)

