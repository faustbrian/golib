package valkey

import valkeygo "github.com/valkey-io/valkey-go"

var scripts = map[operation]*valkeygo.Lua{
	operationAcquire:   valkeygo.NewLuaScript(acquireScript),
	operationHeartbeat: valkeygo.NewLuaScript(heartbeatScript),
	operationInspect:   valkeygo.NewLuaScriptReadOnly(inspectScript),
	operationRelease:   valkeygo.NewLuaScript(releaseScript),
	operationRecover:   valkeygo.NewLuaScript(recoverScript),
}

const luaHelpers = `
local function now_ms()
    local current = redis.call('TIME')
    return current[1] * 1000 + math.floor(current[2] / 1000)
end
local function current_reply()
    return {'ok', redis.call('HGET', KEYS[1], 'logical_key'),
        redis.call('HGET', KEYS[1], 'owner'),
        redis.call('HGET', KEYS[1], 'fencing_token'),
        redis.call('HGET', KEYS[1], 'acquired_at_ms'),
        redis.call('HGET', KEYS[1], 'expires_at_ms')}
end
`

const acquireScript = luaHelpers + `
local now = now_ms()
if redis.call('EXISTS', KEYS[1]) == 1 and
   tonumber(redis.call('HGET', KEYS[1], 'expires_at_ms')) > now then
    return {'error', 'held'}
end
local token = redis.call('INCR', KEYS[2])
local expires = now + tonumber(ARGV[2])
redis.call('HSET', KEYS[1],
    'logical_key', ARGV[3],
    'owner', ARGV[1],
    'fencing_token', tostring(token),
    'acquired_at_ms', tostring(now),
    'expires_at_ms', tostring(expires))
redis.call('PEXPIRE', KEYS[1], ARGV[2])
return current_reply()
`

const heartbeatScript = luaHelpers + `
if redis.call('EXISTS', KEYS[1]) == 0 then return {'error', 'not_found'} end
if redis.call('HGET', KEYS[1], 'owner') ~= ARGV[1] or
   redis.call('HGET', KEYS[1], 'fencing_token') ~= ARGV[2] then
    return {'error', 'stale_owner'}
end
local now = now_ms()
if tonumber(redis.call('HGET', KEYS[1], 'expires_at_ms')) <= now then
    return {'error', 'expired'}
end
local expires = now + tonumber(ARGV[3])
redis.call('HSET', KEYS[1], 'expires_at_ms', tostring(expires))
redis.call('PEXPIRE', KEYS[1], ARGV[3])
return current_reply()
`

const inspectScript = luaHelpers + `
if redis.call('EXISTS', KEYS[1]) == 0 then return {'error', 'not_found'} end
return current_reply()
`

const releaseScript = `
if redis.call('EXISTS', KEYS[1]) == 0 then return {'error', 'not_found'} end
if redis.call('HGET', KEYS[1], 'owner') ~= ARGV[1] or
   redis.call('HGET', KEYS[1], 'fencing_token') ~= ARGV[2] then
    return {'error', 'stale_owner'}
end
redis.call('DEL', KEYS[1])
return {'ok'}
`

const recoverScript = `
if redis.call('EXISTS', KEYS[1]) == 0 then return {'error', 'not_found'} end
if redis.call('HGET', KEYS[1], 'fencing_token') ~= ARGV[1] then
    return {'error', 'stale_owner'}
end
redis.call('DEL', KEYS[1])
return {'ok'}
`
