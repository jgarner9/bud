-- Run with: nvim --clean --headless -l tests/lua/tools_test.lua (just lua-test)
-- The tool loader, and the test-file tool bud ships

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local tmp = vim.fn.tempname()
local shipped, user = tmp .. "/shipped", tmp .. "/user"
local function tool(dir, name, init, manifest)
  vim.fn.mkdir(dir .. "/" .. name, "p")
  vim.fn.writefile(
    { manifest or vim.json.encode({ name = name, description = name .. " tool" }) },
    dir .. "/" .. name .. "/manifest.json"
  )
  vim.fn.writefile(vim.split(init, "\n"), dir .. "/" .. name .. "/init.lua")
end
tool(shipped, "hello", "return { setup = function() vim.g.hello = 'bud' end }")
tool(shipped, "shadowed", "return { setup = function() vim.g.shadow = 'bud' end }")
tool(user, "shadowed", "return { setup = function() vim.g.shadow = 'yours' end }")
tool(user, "off", "return { setup = function() vim.g.off = true end }")
tool(user, "boom", "error('kaboom')")
tool(user, "badjson", "return {}", "{nope")
vim.fn.mkdir(user .. "/languages", "p")

local tools = require("bud.tools")
tools.dirs = function()
  return { { dir = shipped, builtin = true }, { dir = user, builtin = false } }
end
vim.fn.writefile({ vim.json.encode({ tools = { disabled = { "off" } } }) }, tmp .. "/config.json")
require("bud.config").file = tmp .. "/config.json"
-- the CLI isn't there, so a broken tool only notifies
require("bud.cli").bin = function()
  return nil
end

local names = vim.tbl_map(function(t)
  return t.name
end, tools.list())
eq(names, { "badjson", "boom", "hello", "off", "shadowed" }, "every tool, languages/ left out")

local warnings = {}
local notify = vim.notify
vim.notify = function(msg, level)
  if level == vim.log.levels.WARN then
    table.insert(warnings, msg)
  end
end
tools.setup()
vim.notify = notify
eq(vim.g.hello, "bud", "bud's tool loaded")
eq(vim.g.shadow, "yours", "yours wins over bud's")
eq(vim.g.off, nil, "disabled stays off")
eq(tools.is_loaded("boom"), false, "the broken one isn't loaded")
eq(#warnings, 2, "a notification for each broken tool")
eq(warnings[1]:match("badjson") ~= nil and warnings[2]:match("kaboom") ~= nil, true, "saying why")

-- reload picks up a change with no restart
tool(shipped, "hello", "return { setup = function() vim.g.hello = 'again' end }")
eq(tools.reload("hello"), true, "reloaded")
eq(vim.g.hello, "again", "the new version")

-- the test-file tool bud ships
local tf = dofile(vim.fn.getcwd() .. "/tools/test-file/init.lua")
eq(tf.counterpart("main.go"), "main_test.go", "go")
eq(tf.counterpart("main_test.go"), "main.go", "go back")
eq(tf.counterpart("app.py"), "test_app.py", "python")
eq(tf.counterpart("test_app.py"), "app.py", "python back")
eq(tf.counterpart("view.tsx"), "view.test.tsx", "tsx")
eq(tf.counterpart("view.spec.ts"), "view.ts", "spec back")
eq(tf.counterpart("README.md"), nil, "no rule")
local manifest = vim.json.decode(table.concat(vim.fn.readfile(vim.fn.getcwd() .. "/tools/test-file/manifest.json"), ""))
eq(manifest.name, "test-file", "its manifest")

vim.fn.delete(tmp, "rf")
print("tools_test: ok")
