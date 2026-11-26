-- Run with: nvim --clean --headless -l tests/lua/chat_key_test.lua (just lua-test)
-- <leader>ac opens opencode.nvim (D150) and starts no ACP session

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp, "p")
require("bud.config").file = tmp .. "/config.json"

local term = require("bud.ai.term")
-- nvim stands in for the opencode executable
require("bud.ai.opencode").executable = vim.v.progpath
local agent = require("bud.ai.agent")
local opened, toggled, sessions = 0, 0, 0
vim.api.nvim_create_user_command("Opencode", function()
  opened = opened + 1
end, { nargs = "*" })
-- agent.session is the ACP entry point; counting it catches a key that
-- starts `opencode acp` behind opencode.nvim's back
local toggle, session = term.toggle, agent.session
term.toggle = function()
  toggled = toggled + 1
end
agent.session = function()
  sessions = sessions + 1
end

term.chat_key()
eq({ opened, toggled, sessions }, { 1, 0, 0 }, "opens opencode.nvim only")

term.toggle, agent.session = toggle, session
print("ok")
