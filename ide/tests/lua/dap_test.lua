-- Run with: nvim --clean --headless -l tests/lua/dap_test.lua (just lua-test)
-- Debug adapters: what a spec's dap field turns into for nvim-dap

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local adapters = require("bud.dap.adapters")
local dap = { adapters = {}, configurations = { go = { { name = "mine" } } } }
adapters.setup(dap, { "delve", "debugpy", "not-known" })
eq(dap.adapters.delve.type, "server", "delve is a server")
eq(dap.adapters.python.command, "debugpy-adapter", "debugpy runs its adapter")
eq(dap.configurations.go[1].name, "mine", "configurations you had stay first")
eq(#dap.configurations.go, 4, "plus delve's")
eq(dap.configurations.python[1].program, "${file}", "debug this file")
eq(dap.adapters["not-known"], nil, "an unknown adapter adds nothing")

-- every shipped spec's adapter is one bud knows how to run
for _, file in ipairs(vim.fn.glob("languages/*.lua", false, true)) do
  local spec = dofile(file)
  if spec.dap then
    eq(adapters[spec.dap] ~= nil, true, file .. "'s adapter " .. spec.dap)
  end
end

-- collect takes dap's single name as a list of one
local lang = require("bud.lang")
lang.enabled = function()
  return { { dap = "delve" }, { dap = "delve" }, { dap = "codelldb" }, {} }
end
eq(lang.collect("dap"), { "delve", "codelldb" }, "collect a single-name field")

-- the debug layout is valid
local spec = dofile("layouts/debug.lua")
eq(require("bud.layout").validate(spec), nil, "debug layout")

print("dap_test: ok")
