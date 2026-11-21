-- Run with: nvim --clean --headless -l tests/lua/planning_test.lua (just lua-test)
-- Home planning (D110, JGA-293): <leader>aP checks the planning server with
-- its password before starting `opencode --server`, says why when it can't,
-- and keeps the password off every command line

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local function wait(what, fn)
  if not vim.wait(5000, fn, 20) then
    error("timed out waiting for " .. what, 2)
  end
end

local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp, "p")
require("bud.config").file = tmp .. "/config.json"
local function settings(planning)
  vim.fn.writefile({ vim.json.encode({ ai = { planning = planning } }) }, tmp .. "/config.json")
end

local function script(name, body)
  local path = tmp .. "/" .. name
  vim.fn.writefile(vim.split(body, "\n"), path)
  vim.uv.fs_chmod(path, tonumber("755", 8))
  return path
end

-- the fake curl saves its arguments and stdin, then answers with $FAKE_CODE
local curl = script(
  "curl",
  ([[#!/bin/sh
printf '%%s\n' "$@" > %s/curl.args
cat > %s/curl.stdin
printf '%%s' "${FAKE_CODE:-200}"]]):format(tmp, tmp)
)
-- the fake opencode saves its arguments and the password it was given
local opencode = script(
  "opencode",
  ([[#!/bin/sh
printf '%%s\n' "$@" > %s/opencode.args
printf '%%s' "$OPENCODE_PASSWORD" > %s/opencode.pw
exec sleep 30]]):format(tmp, tmp)
)

local planning = require("bud.ai.planning")
planning.curl, planning.opencode = curl, opencode

local notes = {}
vim.notify = function(msg)
  table.insert(notes, msg)
end
local function last_note()
  return notes[#notes] or ""
end

local pwfile = tmp .. "/planning-password"
vim.fn.writefile({ 's3cr"et' }, pwfile)
vim.uv.fs_chmod(pwfile, tonumber("600", 8))

-- no URL: the key says how to set it, and nothing runs
settings({ password_file = pwfile })
planning.open()
assert(last_note():find("home planning unavailable: ai.planning.url isn't set", 1, true), last_note())
eq(planning.statusline(), "plan:off", "a failed check shows off")

-- a password file others can read is refused
settings({ url = "http://100.64.0.1:8797/", password_file = pwfile })
vim.uv.fs_chmod(pwfile, tonumber("644", 8))
planning.open()
assert(last_note():find("chmod 600", 1, true), last_note())
vim.uv.fs_chmod(pwfile, tonumber("600", 8))

-- the server refuses the password
vim.env.FAKE_CODE = "401"
local before = #notes
planning.open()
wait("the 401", function()
  return #notes > before
end)
assert(last_note():find("refused the password", 1, true), last_note())
eq(vim.fn.filereadable(tmp .. "/opencode.args"), 0, "nothing starts after a failed check")

-- a healthy server: the TUI attaches, with the password in its environment.
-- A second press while the check is in flight starts nothing more
vim.env.FAKE_CODE = "200"
planning.open()
planning.open()
wait("the TUI", function()
  return vim.fn.filereadable(tmp .. "/opencode.pw") == 1 and vim.fn.readfile(tmp .. "/opencode.pw")[1] ~= nil
end)
eq(vim.fn.readfile(tmp .. "/opencode.args"), { "--server", "http://100.64.0.1:8797" }, "opencode's arguments")
eq(vim.fn.readfile(tmp .. "/opencode.pw"), { 's3cr"et' }, "the password reaches opencode")
eq(vim.fn.readfile(tmp .. "/curl.stdin"), { 'user = "opencode:s3cr\\"et"' }, "curl gets the password on stdin")
local args = vim.fn.readfile(tmp .. "/curl.args")
eq(args[#args], "http://100.64.0.1:8797/api/info", "curl checks /api/info")
for _, a in ipairs(args) do
  assert(not a:find("s3cr", 1, true), "the password is on curl's command line")
end
eq(planning.statusline(), "plan:home", "a running pane shows home")

-- the toggle hides the pane and keeps the TUI running, then shows it again
local wins = #vim.api.nvim_tabpage_list_wins(0)
eq(wins, 2, "the pane opened beside the editor")
planning.toggle()
eq(#vim.api.nvim_tabpage_list_wins(0), 1, "hidden")
eq(planning.statusline(), "plan:home", "still running while hidden")
planning.toggle()
eq(#vim.api.nvim_tabpage_list_wins(0), 2, "shown again")

-- quitting stops the TUI and hands quit its job to wait on
local jobs = planning.stop_all()
eq(#jobs, 1, "quit gets the planning job")
wait("the TUI to exit", function()
  return planning.statusline() == ""
end)
eq(#vim.api.nvim_tabpage_list_wins(0), 1, "the pane closed with it")

print("ok")
