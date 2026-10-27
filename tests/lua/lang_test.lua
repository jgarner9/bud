-- Run with: nvim --clean --headless -l tests/lua/lang_test.lua (just lua-test)

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local tmp = vim.fn.tempname()
local dir = tmp .. "/languages"
vim.fn.mkdir(dir, "p")

local lang = require("bud.lang")
lang.dirs = { dir }
lang.config_file = tmp .. "/config.json"

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local function write(path, text)
  local f = assert(io.open(path, "w"))
  f:write(text)
  f:close()
end

local function names()
  return vim.tbl_map(function(spec)
    return spec.name
  end, lang.enabled())
end

-- errors and warnings are collected instead of printed
local notified = {}
vim.notify = function(msg)
  table.insert(notified, msg)
end

-- the shipped template has to pass validation, since people copy it
local template = dofile(vim.fn.getcwd() .. "/languages/_template.lua")
write(dir .. "/tmpl.lua", "return " .. vim.inspect(template))
lang.reload()
eq(names(), { "tmpl" }, "the template is a valid spec")
os.remove(dir .. "/tmpl.lua")

write(dir .. "/_template.lua", 'return { filetypes = { "x" } }')
write(
  dir .. "/go.lua",
  [[return {
  filetypes = { "go", "gomod" },
  treesitter = { "go", "gomod" },
  lsp = { gopls = { settings = { gopls = { gofumpt = true } } } },
  lint = { "golangcilint" },
  format = { go = { "goimports", "gofumpt" } },
  mason = { "gopls", "goimports" },
  root = { "go.mod" },
}]]
)
write(
  dir .. "/bash.lua",
  [[return {
  filetypes = { "sh", "bash" },
  lsp = { bashls = {} },
  lint = { "shellcheck" },
  format = { "shfmt" },
  mason = { "bash-language-server", "shfmt" },
  minimal = true,
}]]
)
-- shares sh with bash, to check lists are joined
write(dir .. "/posix.lua", [[return { filetypes = { "sh" }, format = { "shfmt", "beautysh" }, minimal = true }]])

-- no config.json: the full preset enables every spec, by name, and skips _ files
lang.reload()
eq(names(), { "bash", "go", "posix" }, "full preset")
eq(lang.format_on_save(), true, "format on save defaults on")
eq(#notified, 0, "nothing reported")

-- the minimal profile's preset is the specs with minimal = true
write(lang.config_file, '{"profile": "minimal"}')
lang.reload()
eq(names(), { "bash", "posix" }, "minimal preset")

-- a list picks the languages and their order; unknown names are kept aside
write(lang.config_file, '{"profile": "minimal", "languages": ["go", "zig", "bash"], "format_on_save": false}')
lang.reload()
eq(names(), { "go", "bash" }, "configured list")
eq(lang.unknown, { "zig" }, "unknown names")
eq(lang.format_on_save(), false, "format on save off")
eq(lang.specs().posix ~= nil, true, "disabled specs are still loaded")

-- what the plugins wire up
write(lang.config_file, '{"languages": ["go", "bash", "posix"]}')
lang.reload()
eq(lang.filetypes(), { go = true, gomod = true, sh = true, bash = true }, "filetypes")
-- go's format is by filetype, so gomod gets none
eq(lang.by_ft("format"), {
  go = { "goimports", "gofumpt" },
  sh = { "shfmt", "beautysh" },
  bash = { "shfmt" },
}, "formatters by filetype")
eq(lang.by_ft("lint").gomod, { "golangcilint" }, "linters by filetype")
eq(lang.collect("mason"), { "gopls", "goimports", "bash-language-server", "shfmt" }, "mason packages")
eq(lang.collect("treesitter"), { "go", "gomod" }, "parsers")
eq(lang.servers(), {
  gopls = { settings = { gopls = { gofumpt = true } } },
  bashls = {},
}, "servers")

-- format markers by filetype, joined across specs
write(dir .. "/cee.lua", [[return { filetypes = { "c", "h" }, format_markers = { ".clang-format" } }]])
write(dir .. "/cee2.lua", [[return { filetypes = { "c" }, format_markers = { "_clang-format" } }]])
write(lang.config_file, '{"languages": ["go", "cee", "cee2"]}')
lang.reload()
eq(lang.format_markers("c"), { ".clang-format", "_clang-format" }, "markers joined")
eq(lang.format_markers("h"), { ".clang-format" }, "markers for one spec")
eq(lang.format_markers("go"), nil, "no markers")
os.remove(dir .. "/cee.lua")
os.remove(dir .. "/cee2.lua")

-- an empty languages list enables nothing
write(lang.config_file, '{"languages": []}')
lang.reload()
eq(names(), {}, "empty list")

-- a broken spec is reported and left out; the rest still load
os.remove(lang.config_file)
local broken = {
  typo = [[return { filetypes = { "t" }, formatter = { "x" } }]],
  nofts = [[return { lint = { "x" } }]],
  wrongtype = [[return { filetypes = "t" }]],
  badlsp = [[return { filetypes = { "t" }, lsp = { "server" } }]],
  syntax = [[return {]],
  notable = [[return "go"]],
  badft = [[return { filetypes = { "t" }, format = { u = { "x" } } }]],
  badbyft = [[return { filetypes = { "t" }, lint = { t = "x" } }]],
}
for name, text in pairs(broken) do
  write(dir .. "/" .. name .. ".lua", text)
end
notified = {}
lang.reload()
eq(names(), { "bash", "go", "posix" }, "valid specs survive broken ones")
local failed = vim.deepcopy(lang.failed)
table.sort(failed)
eq(failed, { "badbyft", "badft", "badlsp", "nofts", "notable", "syntax", "typo", "wrongtype" }, "failed specs")
eq(#notified, 8, "each broken spec is reported")
eq(
  vim.tbl_contains(notified, [[bud: language badft: format names filetype "u", which isn't in filetypes]]),
  true,
  "names the stray filetype"
)
eq(vim.tbl_contains(notified, 'bud: language typo: unknown field "formatter"'), true, "names the bad field")

eq(lang.errors.typo, 'unknown field "formatter"', "errors say why")
for name in pairs(broken) do
  os.remove(dir .. "/" .. name .. ".lua")
end

-- an earlier dir wins on a name clash, even when its file sorts later
local first = tmp .. "/zz-first"
vim.fn.mkdir(first, "p")
write(first .. "/go.lua", [[return { filetypes = { "first" } }]])
lang.dirs = { first, dir }
lang.reload()
eq(lang.specs().go.filetypes, { "first" }, "the earlier dir wins")
eq(lang.specs().bash ~= nil, true, "the later dir's other specs still load")
lang.dirs = { dir }
for name, text in pairs(broken) do
  write(dir .. "/" .. name .. ".lua", text)
end

-- a broken spec named in the list isn't also unknown
write(lang.config_file, '{"languages": ["typo"]}')
lang.reload()
eq(lang.unknown, {}, "failed isn't unknown")

-- a corrupt config.json warns and falls back to the preset
write(lang.config_file, "{not json")
notified = {}
lang.reload()
eq(names(), { "bash", "go", "posix" }, "corrupt config uses the preset")
eq(#vim.tbl_filter(function(msg)
  return msg:find("can't read", 1, true) ~= nil
end, notified), 1, "corrupt config is reported")

print("lang_test: ok")
