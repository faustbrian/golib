package valkey

import valkeygo "github.com/valkey-io/valkey-go"

var nativeScripts = map[operation]*valkeygo.Lua{
	operationAcquire:   valkeygo.NewLuaScript(acquireScript),
	operationInspect:   valkeygo.NewLuaScriptReadOnly(inspectScript),
	operationHeartbeat: valkeygo.NewLuaScript(heartbeatScript),
	operationComplete:  valkeygo.NewLuaScript(completeScript),
	operationFail:      valkeygo.NewLuaScript(failScript),
	operationRelease:   valkeygo.NewLuaScript(releaseScript),
	operationExpire:    valkeygo.NewLuaScript(expireScript),
}

const acquireScript = `
local function now_ms()
    local current = redis.call('TIME')
    return current[1] * 1000 + math.floor(current[2] / 1000)
end
local function record_reply(outcome)
    local fields = redis.call('HGETALL', KEYS[1])
    table.insert(fields, 1, outcome)
    return fields
end

local now = now_ms()
local lease_ms = tonumber(ARGV[7])
local retention_ms = tonumber(ARGV[8])
if redis.call('EXISTS', KEYS[1]) == 0 then
    redis.call('HSET', KEYS[1],
        'schema', '1',
        'namespace', ARGV[1],
        'tenant', ARGV[2],
        'operation', ARGV[3],
        'caller', ARGV[4],
        'key_value', ARGV[5],
        'fingerprint_version', ARGV[9],
        'fingerprint_sum', ARGV[10],
        'state', 'acquired',
        'owner_token', ARGV[6],
        'fencing_token', '1',
        'lease_expires_at_ms', tostring(now + lease_ms),
        'heartbeat_at_ms', tostring(now),
        'attempt', '1',
        'created_at_ms', tostring(now),
        'updated_at_ms', tostring(now),
        'completed_at_ms', '0',
        'failed_at_ms', '0',
        'abandoned_at_ms', '0',
        'expired_at_ms', '0',
        'result', '',
        'metadata', '{}')
    redis.call('PEXPIRE', KEYS[1], lease_ms + retention_ms)
    return record_reply('acquired')
end

if redis.call('HGET', KEYS[1], 'fingerprint_version') ~= ARGV[9] or
   redis.call('HGET', KEYS[1], 'fingerprint_sum') ~= ARGV[10] then
    return record_reply('conflict')
end

local state = redis.call('HGET', KEYS[1], 'state')
if state == 'completed' then
    return record_reply('replayed')
end
if state == 'failed' then
    return record_reply('terminal_failure')
end
if (state == 'acquired' or state == 'running') and
   tonumber(redis.call('HGET', KEYS[1], 'lease_expires_at_ms')) > now then
    return record_reply('in_progress')
end

local outcome = 'acquired'
if state == 'acquired' or state == 'running' then
    outcome = 'stale_owner_takeover'
end
redis.call('HINCRBY', KEYS[1], 'fencing_token', 1)
redis.call('HINCRBY', KEYS[1], 'attempt', 1)
redis.call('HSET', KEYS[1],
    'state', 'acquired',
    'owner_token', ARGV[6],
    'lease_expires_at_ms', tostring(now + lease_ms),
    'heartbeat_at_ms', tostring(now),
    'updated_at_ms', tostring(now),
    'completed_at_ms', '0',
    'failed_at_ms', '0',
    'abandoned_at_ms', '0',
    'expired_at_ms', '0',
    'result', '',
    'metadata', '{}')
redis.call('PEXPIRE', KEYS[1], lease_ms + retention_ms)
return record_reply(outcome)
`

const inspectScript = `
if redis.call('EXISTS', KEYS[1]) == 0 then
    return {'error', 'not_found'}
end
local fields = redis.call('HGETALL', KEYS[1])
table.insert(fields, 1, 'ok')
return fields
`

const heartbeatScript = `
local function now_ms()
    local current = redis.call('TIME')
    return current[1] * 1000 + math.floor(current[2] / 1000)
end
local function ok()
    local fields = redis.call('HGETALL', KEYS[1])
    table.insert(fields, 1, 'ok')
    return fields
end
if redis.call('EXISTS', KEYS[1]) == 0 then return {'error', 'not_found'} end
if redis.call('HGET', KEYS[1], 'owner_token') ~= ARGV[1] or
   redis.call('HGET', KEYS[1], 'fencing_token') ~= ARGV[2] then
    return {'error', 'stale_owner'}
end
local state = redis.call('HGET', KEYS[1], 'state')
if state ~= 'acquired' and state ~= 'running' then return {'error', 'invalid_transition'} end
local now = now_ms()
if now >= tonumber(redis.call('HGET', KEYS[1], 'lease_expires_at_ms')) then
    return {'error', 'lease_expired'}
end
local lease_ms = tonumber(ARGV[3])
redis.call('HSET', KEYS[1],
    'state', 'running',
    'heartbeat_at_ms', tostring(now),
    'lease_expires_at_ms', tostring(now + lease_ms),
    'updated_at_ms', tostring(now))
redis.call('PEXPIRE', KEYS[1], lease_ms + tonumber(ARGV[4]))
return ok()
`

const completeScript = `
local function now_ms()
    local current = redis.call('TIME')
    return current[1] * 1000 + math.floor(current[2] / 1000)
end
local function ok()
    local fields = redis.call('HGETALL', KEYS[1])
    table.insert(fields, 1, 'ok')
    return fields
end
if redis.call('EXISTS', KEYS[1]) == 0 then return {'error', 'not_found'} end
if redis.call('HGET', KEYS[1], 'owner_token') ~= ARGV[1] or
   redis.call('HGET', KEYS[1], 'fencing_token') ~= ARGV[2] then
    return {'error', 'stale_owner'}
end
local state = redis.call('HGET', KEYS[1], 'state')
if state ~= 'acquired' and state ~= 'running' then return {'error', 'invalid_transition'} end
local now = now_ms()
if now >= tonumber(redis.call('HGET', KEYS[1], 'lease_expires_at_ms')) then
    return {'error', 'lease_expired'}
end
redis.call('HSET', KEYS[1],
    'state', 'completed',
    'result', ARGV[3],
    'metadata', ARGV[4],
    'completed_at_ms', tostring(now),
    'updated_at_ms', tostring(now))
redis.call('PEXPIRE', KEYS[1], tonumber(ARGV[5]))
return ok()
`

const failScript = `
local function now_ms()
    local current = redis.call('TIME')
    return current[1] * 1000 + math.floor(current[2] / 1000)
end
local function ok()
    local fields = redis.call('HGETALL', KEYS[1])
    table.insert(fields, 1, 'ok')
    return fields
end
if redis.call('EXISTS', KEYS[1]) == 0 then return {'error', 'not_found'} end
if redis.call('HGET', KEYS[1], 'owner_token') ~= ARGV[1] or
   redis.call('HGET', KEYS[1], 'fencing_token') ~= ARGV[2] then
    return {'error', 'stale_owner'}
end
local state = redis.call('HGET', KEYS[1], 'state')
if state ~= 'acquired' and state ~= 'running' then return {'error', 'invalid_transition'} end
local now = now_ms()
if now >= tonumber(redis.call('HGET', KEYS[1], 'lease_expires_at_ms')) then
    return {'error', 'lease_expired'}
end
redis.call('HSET', KEYS[1],
    'state', 'failed',
    'result', ARGV[3],
    'metadata', ARGV[4],
    'failed_at_ms', tostring(now),
    'updated_at_ms', tostring(now))
redis.call('PEXPIRE', KEYS[1], tonumber(ARGV[5]))
return ok()
`

const releaseScript = `
local function now_ms()
    local current = redis.call('TIME')
    return current[1] * 1000 + math.floor(current[2] / 1000)
end
local function ok()
    local fields = redis.call('HGETALL', KEYS[1])
    table.insert(fields, 1, 'ok')
    return fields
end
if redis.call('EXISTS', KEYS[1]) == 0 then return {'error', 'not_found'} end
if redis.call('HGET', KEYS[1], 'owner_token') ~= ARGV[1] or
   redis.call('HGET', KEYS[1], 'fencing_token') ~= ARGV[2] then
    return {'error', 'stale_owner'}
end
local state = redis.call('HGET', KEYS[1], 'state')
if state ~= 'acquired' and state ~= 'running' then return {'error', 'invalid_transition'} end
local now = now_ms()
if now >= tonumber(redis.call('HGET', KEYS[1], 'lease_expires_at_ms')) then
    return {'error', 'lease_expired'}
end
redis.call('HSET', KEYS[1],
    'state', 'abandoned',
    'abandoned_at_ms', tostring(now),
    'updated_at_ms', tostring(now))
redis.call('PEXPIRE', KEYS[1], tonumber(ARGV[3]))
return ok()
`

const expireScript = `
local function now_ms()
    local current = redis.call('TIME')
    return current[1] * 1000 + math.floor(current[2] / 1000)
end
local function ok()
    local fields = redis.call('HGETALL', KEYS[1])
    table.insert(fields, 1, 'ok')
    return fields
end
if redis.call('EXISTS', KEYS[1]) == 0 then return {'error', 'not_found'} end
local state = redis.call('HGET', KEYS[1], 'state')
local now = now_ms()
if (state ~= 'acquired' and state ~= 'running') or
   now < tonumber(redis.call('HGET', KEYS[1], 'lease_expires_at_ms')) then
    return {'error', 'invalid_transition'}
end
redis.call('HSET', KEYS[1],
    'state', 'expired',
    'expired_at_ms', tostring(now),
    'updated_at_ms', tostring(now))
redis.call('PEXPIRE', KEYS[1], tonumber(ARGV[1]))
return ok()
`
