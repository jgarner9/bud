-- Dumps language specs as JSON for the Go CLI, so specs have one format and one
-- reader (BASE.md 1). internal/lang runs it as
--   nvim --clean --headless -l dump.lua <root> <out> <dir>...
-- where <root> holds this binary's lua/bud/lang, <out> is the file to write and
-- the dirs are searched like bud.lang's (an earlier dir wins on a name clash).
-- Go works out which languages are enabled itself, so config.json isn't read.

local root, out = arg[1], arg[2]
vim.opt.rtp:prepend(root)

local lang = require("bud.lang")
lang.dirs = { unpack(arg, 3) }
lang.config_file = root .. "/no-config.json"
-- failures come back in lang.errors instead
vim.notify = function() end
lang.reload()

-- only the fields Go reads; lint and format stay Lua's business
local specs = {}
for _, spec in pairs(lang.specs()) do
  table.insert(specs, {
    name = spec.name,
    filetypes = spec.filetypes,
    treesitter = spec.treesitter,
    mason = spec.mason,
    system = spec.system,
    root = spec.root,
    minimal = spec.minimal == true,
    builtin = spec.builtin,
  })
end
table.sort(specs, function(a, b)
  return a.name < b.name
end)

-- an empty table would encode as [], and Go wants an object
local errors = vim.empty_dict()
for name, err in pairs(lang.errors) do
  errors[name] = err
end

local f = assert(io.open(out, "w"))
f:write(vim.json.encode({ specs = specs, errors = errors }))
f:close()
