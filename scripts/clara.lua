-- scripts/clara.lua
-- Helper module for Lua scripts executing under Clara supervision.

local clara = {}

-- Simple JSON decoder/encoder fallback if external cjson is not available
local function parse_json(str)
    -- Try cjson first if installed
    local ok, cjson = pcall(require, "cjson")
    if ok then
        return cjson.decode(str)
    end

    -- Try dkjson if installed
    local ok2, dkjson = pcall(require, "dkjson")
    if ok2 then
        return dkjson.decode(str)
    end

    -- Very basic regex-based parser or fallback: return raw string wrapper
    -- In practice, lua-cjson or python bridge can be used.
    error("JSON library (cjson or dkjson) required to parse JSON in Lua. Install lua-cjson via luarocks or homebrew.")
end

local function encode_json(tbl)
    local ok, cjson = pcall(require, "cjson")
    if ok then
        return cjson.encode(tbl)
    end
    local ok2, dkjson = pcall(require, "dkjson")
    if ok2 then
        return dkjson.encode(tbl)
    end
    error("JSON library (cjson or dkjson) required to encode JSON in Lua.")
end

-- Get incoming event data passed from Clara supervisor
function clara.get_event()
    local env_json = os.getenv("CLARA_EVENT_JSON")
    if env_json and #env_json > 0 then
        return parse_json(env_json)
    end

    -- Read from stdin
    local input = io.read("*a")
    if input and #input > 0 then
        return parse_json(input)
    end

    return {}
end

-- Call a Clara MCP tool via the clara CLI
function clara.call_tool(tool_name, args)
    local args_parts = {}
    for k, v in pairs(args or {}) do
        local val_str = type(v) == "table" and encode_json(v) or tostring(v)
        -- Escape single quotes in value
        val_str = val_str:gsub("'", "'\\''")
        table.insert(args_parts, string.format("%s='%s'", k, val_str))
    end

    local cmd = string.format("clara tool call %s %s", tool_name, table.concat(args_parts, " "))
    local handle = io.popen(cmd)
    if not handle then
        error("failed to execute clara tool command: " .. cmd)
    end

    local output = handle:read("*a")
    local success, exit_type, code = handle:close()

    if not success then
        error(string.format("tool %s failed with code %s: %s", tool_name, tostring(code), output))
    end

    -- Parse tool response JSON if possible
    local ok, result = pcall(parse_json, output)
    if ok then
        return result
    end
    return output
end

function clara.run_id()
    return os.getenv("CLARA_RUN_ID") or "unknown"
end

function clara.trigger_id()
    return os.getenv("CLARA_TRIGGER_ID") or "unknown"
end

return clara
