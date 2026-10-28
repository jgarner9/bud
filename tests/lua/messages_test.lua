-- Run with: nvim --clean --headless -l tests/lua/messages_test.lua (just lua-test)

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local config = require("bud.config")
local messages = require("bud.messages")
local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp, "p")
config.file = tmp .. "/config.json"

-- this Neovim has ui2 under one of its names
assert(messages.module(), "no ui2 module")

-- turned off: not enabled
local enabled = false
local orig = messages.module
messages.module = function()
  return {
    enable = function()
      enabled = true
    end,
  }
end
vim.fn.writefile({ '{"ui": {"messages": false}}' }, config.file)
assert(messages.setup() == false and not enabled, "enabled while off")

-- on by default
vim.fn.delete(config.file)
assert(messages.setup() == true and enabled, "not enabled by default")
messages.module = orig

print("messages_test: ok")
