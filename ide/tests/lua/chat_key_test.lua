-- Run with: nvim --clean --headless -l tests/lua/chat_key_test.lua (just lua-test)
-- <leader>ac on the opencode backend opens opencode.nvim (D150) and starts no
-- ACP session; on any other backend it is the pane toggle as before

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp, "p")
require("bud.config").file = tmp .. "/config.json"
local function backend(name)
  vim.fn.writefile({ vim.json.encode({ ai = { backend = name } }) }, tmp .. "/config.json")
end

local term = require("bud.ai.term")
local agent = require("bud.ai.agent")
local opened, toggled, sessions = 0, 0, 0
vim.api.nvim_create_user_command("Opencode", function()
  opened = opened + 1
end, { nargs = "*" })
local toggle, ensure = term.toggle, agent.ensure
term.toggle = function()
  toggled = toggled + 1
end
if ensure then
  agent.ensure = function()
    sessions = sessions + 1
  end
end

backend("opencode")
term.chat_key()
eq({ opened, toggled, sessions }, { 1, 0, 0 }, "opencode opens opencode.nvim only")

backend("claude")
term.chat_key()
eq({ opened, toggled, sessions }, { 1, 1, 0 }, "claude toggles the pane")

term.toggle, agent.ensure = toggle, ensure
print("ok")
