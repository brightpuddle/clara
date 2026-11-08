-- Clara agent session picker and statusline component for Neovim.
--
-- Install: copy or symlink this file to ~/.config/nvim/lua/clara_agents.lua, then
--   require("clara_agents").setup()            -- keymap <leader>aa (picker)
--   lualine_x = { require("clara_agents").status }
--
-- Needs `clara` on PATH, the daemon running, and fzf-lua (falls back to vim.ui.select).
local M = {}

local cache = { text = "", at = 0 }
local TTL_MS = 5000

local function sessions(cb)
  vim.system({ "clara", "agent", "list", "--json" }, { text = true }, function(res)
    vim.schedule(function()
      if res.code ~= 0 then
        return cb(nil, vim.trim(res.stderr or "clara agent list failed"))
      end
      local ok, list = pcall(vim.json.decode, res.stdout)
      if not ok then
        return cb(nil, "bad JSON from clara")
      end
      cb(list)
    end)
  end)
end

local function label(s)
  local dir = vim.fn.fnamemodify(s.dir or "", ":~")
  return string.format("%-9s %-8s %s  %s", s.agent or "?", s.state or "?", dir, s.title or "")
end

local function attach(s)
  if s.agent ~= "opencode" then
    vim.notify("clara: only opencode sessions can be attached here", vim.log.levels.WARN)
    return
  end
  vim.cmd("tabnew")
  vim.fn.termopen({ "opencode", "attach", "--session", s.id }, { cwd = s.dir })
  vim.cmd("startinsert")
end

function M.pick()
  sessions(function(list, err)
    if not list then
      return vim.notify("clara: " .. err, vim.log.levels.ERROR)
    end
    if #list == 0 then
      return vim.notify("clara: no sessions")
    end
    vim.ui.select(list, { prompt = "Agent session", format_item = label }, function(s)
      if s then
        attach(s)
      end
    end)
  end)
end

-- Lualine component: cached, refreshed asynchronously so the statusline never blocks.
function M.status()
  local now = vim.uv.now()
  if now - cache.at > TTL_MS then
    cache.at = now
    vim.system({ "clara", "agent", "status", "--short" }, { text = true }, function(res)
      cache.text = res.code == 0 and vim.trim(res.stdout or "") or ""
    end)
  end
  return cache.text ~= "" and ("󰚩 " .. cache.text) or ""
end

function M.setup(opts)
  opts = opts or {}
  vim.keymap.set("n", opts.key or "<leader>aa", M.pick, { desc = "Clara agent sessions" })
  vim.api.nvim_create_user_command("ClaraAgents", M.pick, {})
end

return M
