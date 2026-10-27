-- Run with: nvim --clean --headless -l tests/lua/pack_test.lua (just lua-test)
-- Builds throwaway plugins as local git repos and loads them through bud.pack.

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local tmp = vim.fn.tempname()
vim.env.XDG_CONFIG_HOME = tmp .. "/config"
vim.env.XDG_DATA_HOME = tmp .. "/data"
vim.env.XDG_STATE_HOME = tmp .. "/state"
vim.opt.packpath:prepend(tmp .. "/data/nvim/site")
-- vim.pack writes its lockfile into stdpath("config"): never let that be a real one
for _, what in ipairs({ "config", "data" }) do
  assert(vim.startswith(vim.fn.stdpath(what), tmp), what .. " dir is not the temp dir, refusing to run")
end

local function sh(cmd)
  local out = vim.system(cmd, { text = true }):wait()
  assert(out.code == 0, table.concat(cmd, " ") .. ": " .. (out.stderr or ""))
end

-- repo creates a git repo holding files (path -> contents) and returns its path
local function repo(name, files)
  local dir = tmp .. "/repos/" .. name
  for path, body in pairs(files) do
    vim.fn.mkdir(vim.fs.dirname(dir .. "/" .. path), "p")
    vim.fn.writefile(vim.split(body, "\n"), dir .. "/" .. path)
  end
  sh({ "git", "-C", dir, "init", "-q" })
  sh({ "git", "-C", dir, "add", "." })
  -- a throwaway repo: don't sign even when the user's git config says to
  local ident = { "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false" }
  sh(vim.list_extend({ "git", "-C", dir }, vim.list_extend(ident, { "commit", "-q", "-m", "x" })))
  return dir
end

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local eager = repo("eager", {
  ["plugin/eager.lua"] = "vim.g.eager_sourced = true",
  ["lua/eager.lua"] = "return { ok = true }",
})
local bycmd = repo("bycmd", {
  ["plugin/bycmd.lua"] = [[
vim.g.bycmd_sourced = (vim.g.bycmd_sourced or 0) + 1
vim.api.nvim_create_user_command("ByCmd", function(a) vim.g.bycmd_args = a.fargs end, { nargs = "*" })]],
})
local byevent = repo("byevent", {
  ["plugin/byevent.lua"] = [[
vim.g.byevent_seen = 0
vim.api.nvim_create_autocmd("InsertEnter", {
  group = vim.api.nvim_create_augroup("byevent", {}),
  callback = function() vim.g.byevent_seen = vim.g.byevent_seen + 1 end,
})]],
})
local bykey = repo("bykey", { ["lua/bykey.lua"] = "return {}" })

-- something else listening on InsertEnter, which the replay must not run again
local others = 0
vim.api.nvim_create_autocmd("InsertEnter", {
  callback = function()
    others = others + 1
  end,
})

local pack = require("bud.pack")
local configs = {}
-- the broken plugin below reports its error through vim.notify; keep the output clean
local notified = {}
local notify = vim.notify
vim.notify = function(msg)
  table.insert(notified, msg)
end
pack.setup({
  {
    src = eager,
    config = function()
      configs.eager = require("eager").ok
    end,
  },
  {
    src = bycmd,
    cmd = "ByCmd",
    init = function()
      configs.bycmd_init = true
    end,
    config = function()
      configs.bycmd = true
    end,
  },
  {
    src = byevent,
    event = "InsertEnter",
    config = function()
      configs.byevent = true
    end,
  },
  {
    src = bykey,
    keys = { { "gz", desc = "test key" } },
    config = function()
      vim.keymap.set("n", "gz", function()
        vim.g.bykey_hit = (vim.g.bykey_hit or 0) + 1
      end)
    end,
  },
  {
    src = repo("broken", { ["lua/broken.lua"] = "return {}" }),
    config = function()
      error("boom")
    end,
  },
})

vim.notify = notify
-- vim.pack's install progress goes through vim.notify too
local reported = vim.tbl_filter(function(msg)
  return msg:find("setting up broken failed", 1, true) ~= nil
end, notified)
eq(#reported, 1, "the broken config was reported")

-- eager: sourced and configured straight away (after startup, so no :packadd!)
eq(vim.g.eager_sourced, true, "eager plugin/ sourced")
eq(configs.eager, true, "eager config ran")

-- a broken config is recorded and doesn't stop the others
eq(pack.failed, { "broken" }, "failed configs")

-- lazy: init ran, nothing else yet
eq(configs.bycmd_init, true, "init runs at startup")
eq({ vim.g.bycmd_sourced, configs.bycmd, vim.g.byevent_seen, configs.byevent }, {}, "nothing lazy loaded yet")

-- cmd: the stub loads the plugin and runs the real command with the same args
vim.cmd("ByCmd a b")
eq(vim.g.bycmd_args, { "a", "b" }, "real command got the args")
eq({ vim.g.bycmd_sourced, configs.bycmd }, { 1, true }, "loaded once via its command")
vim.cmd("ByCmd c")
eq(vim.g.bycmd_args, { "c" }, "the real command replaced the stub")
pack.load("bycmd")
eq(vim.g.bycmd_sourced, 1, "load is idempotent")

-- event: loads on the first one, and the plugin sees that event exactly once
vim.api.nvim_exec_autocmds("InsertEnter", {})
eq({ configs.byevent, vim.g.byevent_seen }, { true, 1 }, "loaded and replayed once")
eq(others, 1, "replay leaves other handlers alone")
vim.api.nvim_exec_autocmds("InsertEnter", {})
eq({ vim.g.byevent_seen, others }, { 2, 2 }, "later events reach it normally")

-- keys: the stub loads the plugin and the key reaches the real mapping
vim.api.nvim_feedkeys("gz", "mx", false)
vim.api.nvim_feedkeys("", "x", false)
eq(vim.g.bykey_hit, 1, "real mapping ran on first press")
vim.api.nvim_feedkeys("gz", "mx", false)
eq(vim.g.bykey_hit, 2, "second press goes straight to it")

print("pack_test: ok")
