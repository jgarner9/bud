-- The tool generator (BASE.md 4 part 2, JGA-91): :BudGen "what you want".
-- Claude writes a tool (manifest.json + init.lua, D54) or a language spec in
-- user-tools/, on its own session there. Every file it writes is a
-- permission diff you accept or reject (bud.ai.permissions), so nothing
-- lands without you seeing it. Then a new or changed tool is loaded in a
-- headless Neovim first: it's turned on only if that passes, and turned off
-- with the error if not.
--
-- The conversation shows as a log in a split, where i asks for changes; each
-- reply runs the checks again.

-- the configured backend's session (D131)
local ai = require("bud.ai.session")

local M = {}

M.key = "gen"

local state = {}

local function user_tools()
  local dir = vim.fn.stdpath("data") .. "/user-tools"
  vim.fn.mkdir(dir .. "/languages", "p")
  return vim.uv.fs_realpath(dir) or dir
end

-- prompt is what Claude is asked: the request, and the contracts it must follow
function M.prompt(request, template)
  return table.concat({
    "You're writing an extension for bud, a Neovim IDE. The request:",
    "",
    request,
    "",
    "Make it either a tool or a language spec, whichever fits, in the current directory (bud's user-tools/).",
    "",
    "A tool is a directory <name>/ (lowercase, dashes) with two files:",
    '- <name>/manifest.json: {"name": "<name>", "description": "<one line>", "commands": [user commands it adds], "keymaps": [{"lhs": "...", "desc": "..."}]}',
    "- <name>/init.lua: returns a table with setup(manifest), called once when the tool loads. Create commands and keymaps there.",
    "  Use only Neovim's Lua API (Neovim 0.12). Prefer user commands named Bud<Something>; avoid <leader> keymaps that",
    "  clash with existing ones (check with :map if unsure). Keep it small and readable, with a comment at the top saying what it does.",
    "",
    "A language spec is one file, languages/<name>.lua, following this template exactly (unknown fields are errors):",
    "```lua",
    template or "",
    "```",
    "",
    "Write the files with your Write tool; the user reviews each one as a diff. Don't run anything else.",
    "When you're done, say in one or two sentences what you made and how to use it.",
  }, "\n")
end

-- snapshot is every tool's init.lua and manifest.json with its mtime, and the
-- language specs, so what Claude changed can be found afterwards
local function snapshot(dir)
  local out = {}
  for name, kind in vim.fs.dir(dir, { depth = 2 }) do
    if
      kind == "file"
      and (name:match("/init%.lua$") or name:match("/manifest%.json$") or name:match("^languages/.+%.lua$"))
    then
      local stat = vim.uv.fs_stat(dir .. "/" .. name)
      out[name] = stat and stat.mtime.sec .. "." .. stat.mtime.nsec
    end
  end
  return out
end

-- changed lists the tools and specs that are new or different since before
function M.changed(before, after)
  local tools, specs, seen = {}, {}, {}
  for name, stamp in pairs(after) do
    if before[name] ~= stamp then
      local spec = name:match("^languages/(.+)%.lua$")
      local tool = not spec and name:match("^([^/]+)/")
      if spec and not seen["l:" .. spec] then
        seen["l:" .. spec] = true
        table.insert(specs, spec)
      elseif tool and not seen[tool] then
        seen[tool] = true
        table.insert(tools, tool)
      end
    end
  end
  table.sort(tools)
  table.sort(specs)
  return tools, specs
end

-- swappable in tests: loads a tool in a fresh headless Neovim and calls back
-- cb(ok, err)
M.load_test = function(dir, cb)
  local script = table.concat({
    "local dir = _G.arg[1]",
    "vim.opt.rtp:prepend(vim.fn.stdpath('config'))",
    "local chunk, err = loadfile(dir .. '/init.lua')",
    "if not chunk then io.stderr:write(err) os.exit(1) end",
    "local ok, mod = pcall(chunk)",
    "if not ok then io.stderr:write(tostring(mod)) os.exit(1) end",
    "if type(mod) == 'table' and type(mod.setup) == 'function' then",
    "  local f = io.open(dir .. '/manifest.json') local m = f and vim.json.decode(f:read('*a')) or {}",
    "  local ok2, err2 = pcall(mod.setup, m)",
    "  if not ok2 then io.stderr:write(tostring(err2)) os.exit(1) end",
    "end",
  }, "\n")
  local file = vim.fn.tempname() .. ".lua"
  vim.fn.writefile(vim.split(script, "\n"), file)
  vim.system({ vim.v.progpath, "--clean", "--headless", "-l", file, dir }, { text = true }, function(res)
    os.remove(file)
    vim.schedule(function()
      cb(res.code == 0, vim.trim(res.stderr or ""))
    end)
  end)
end

-- check loads what Claude made: a tool passes the load test and is turned on,
-- or is turned off with the error; a spec is pointed at bud lang enable
function M.check(before)
  local dir = user_tools()
  local tools, specs = M.changed(before, snapshot(dir))
  local tools_mod = require("bud.tools")
  for _, name in ipairs(tools) do
    M.load_test(dir .. "/" .. name, function(ok, err)
      local bin = require("bud.cli").bin()
      if ok then
        if bin then
          vim.system({ bin, "tools", "enable", name })
        end
        tools_mod.reload(name)
        vim.notify(("bud: the %s tool passed its load test and is on"):format(name))
      else
        if bin then
          vim.system({ bin, "tools", "disable", "--reason", err:gsub("\n.*", ""), name })
        end
        vim.notify(("bud: the %s tool failed its load test, so it's off: %s"):format(name, err), vim.log.levels.WARN)
      end
    end)
  end
  for _, name in ipairs(specs) do
    vim.notify(("bud: wrote the %s language spec; bud lang enable %s installs its tools"):format(name, name))
  end
  return tools, specs
end

local function template()
  local path = vim.fn.stdpath("config") .. "/languages/_template.lua"
  local fd = io.open(path, "r")
  if not fd then
    return nil
  end
  local text = fd:read("*a")
  fd:close()
  return text
end

-- the log: the conversation as plain lines, in a scratch buffer
local function log(lines)
  local buf = state.buf
  if not buf or not vim.api.nvim_buf_is_valid(buf) then
    return
  end
  if type(lines) == "string" then
    lines = vim.split(lines, "\n", { plain = true })
  end
  vim.bo[buf].modifiable = true
  local last = vim.api.nvim_buf_line_count(buf)
  local empty = last == 1 and vim.api.nvim_buf_get_lines(buf, 0, 1, false)[1] == ""
  vim.api.nvim_buf_set_lines(buf, empty and 0 or last, -1, false, lines)
  vim.bo[buf].modifiable = false
  for _, win in ipairs(vim.fn.win_findbuf(buf)) do
    vim.api.nvim_win_set_cursor(win, { vim.api.nvim_buf_line_count(buf), 0 })
  end
end

-- ask sends a follow-up (asking for changes), from the log's i or Enter
local function ask()
  vim.ui.input({ prompt = "Ask for changes: " }, function(text)
    if text and vim.trim(text) ~= "" and ai.send(text, { key = M.key }) then
      log({ "", "> " .. text, "" })
    end
  end)
end

-- show puts the log in a split along the bottom
local function show()
  if #vim.fn.win_findbuf(state.buf) > 0 then
    return
  end
  local win =
    vim.api.nvim_open_win(state.buf, false, { split = "below", win = -1, height = math.floor(vim.o.lines * 0.3) })
  vim.wo[win].winbar = "%#Title# " .. state.title:gsub("%%", "%%%%") .. " %*  i ask for changes  q close"
  vim.wo[win].wrap = true
end

local function close()
  for _, unsub in ipairs(state.unsub or {}) do
    unsub()
  end
  if state.buf and vim.api.nvim_buf_is_valid(state.buf) then
    vim.api.nvim_buf_delete(state.buf, { force = true })
  end
end

-- listen logs what the generator's session says, and checks what Claude
-- wrote after each reply
local function listen(dir)
  local function mine(session)
    return session.key == M.key
  end
  state.unsub = {
    ai.on("message", function(d, session)
      if mine(session) and d.role == "assistant" and d.text then
        log(vim.list_extend(vim.split(d.text, "\n", { plain = true }), { "" }))
      end
    end),
    ai.on("tool_use", function(d, session)
      if mine(session) then
        local input = type(d.input) == "table" and d.input or {}
        local what = input.file_path or input.path or input.command or ""
        log(("● %s %s"):format(d.name, type(what) == "string" and vim.fn.fnamemodify(what, ":~:.") or ""))
      end
    end),
    ai.on("done", function(d, session)
      if mine(session) then
        if not d.ok then
          log({ "✗ " .. (d.text or "Claude stopped"), "" })
        end
        -- each reply may have written files: check them, then compare against now
        M.check(state.before)
        state.before = snapshot(dir)
      end
    end),
  }
end

-- run starts generating from a description: a fresh conversation each time
function M.run(request)
  if not request or vim.trim(request) == "" then
    vim.ui.input({ prompt = "Describe the tool or language: " }, function(text)
      if text and vim.trim(text) ~= "" then
        M.run(text)
      end
    end)
    return
  end
  local dir = user_tools()
  ai.drop(M.key)
  close()
  state = { title = "BudGen: " .. request, buf = vim.api.nvim_create_buf(false, true) }
  vim.bo[state.buf].bufhidden = "hide"
  vim.bo[state.buf].modifiable = false
  for _, lhs in ipairs({ "i", "<CR>" }) do
    vim.keymap.set("n", lhs, ask, { buffer = state.buf, nowait = true, desc = "Ask for changes" })
  end
  vim.keymap.set("n", "q", function()
    for _, win in ipairs(vim.fn.win_findbuf(state.buf)) do
      vim.api.nvim_win_close(win, false)
    end
  end, { buffer = state.buf, nowait = true, desc = "Close the generator's log" })
  state.before = snapshot(dir)
  listen(dir)
  ai.session(dir, { key = M.key, extra = true, resume = "new" })
  show()
  if ai.send(M.prompt(request, template()), { key = M.key }) then
    log({ "> " .. request, "" })
  end
end

function M.setup()
  vim.api.nvim_create_user_command("BudGen", function(o)
    M.run(o.args)
  end, { nargs = "?", desc = "Have Claude write a bud tool or language spec" })
end

-- buf is the generator's log, for tests
function M.buf()
  return state.buf
end

function M._reset()
  close()
  ai.drop(M.key)
  state = {}
end

return M
