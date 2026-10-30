-- The two-key open (BASE.md 5.4, D39): p / <leader>p picks a project, f /
-- <leader>f picks a file. The same functions back the dashboard keys and the
-- leader keys, so they behave the same everywhere.

local projects = require("bud.projects")
local cli = require("bud.cli")
local icons = require("bud.icons")

local M = {}

-- how many projects found through recent files to offer, beyond the registered ones
local max_recent = 10

local function fzf()
  return require("bud.pack").deferred["fzf-lua"]()
end

-- candidates lists what p offers: registered projects (pinned, then recent),
-- then repos holding recently opened files that aren't registered, then a way
-- to type a directory. Each is { kind, name, path }
function M.candidates(oldfiles)
  local out, seen = {}, {}
  for _, p in ipairs(projects.list()) do
    if not p.missing then
      table.insert(out, { kind = p.pinned and "pinned" or "project", name = p.name, path = p.path })
      seen[p.path] = true
    end
  end
  local recent = 0
  for _, file in ipairs(oldfiles or vim.v.oldfiles) do
    if recent >= max_recent then
      break
    end
    local root = vim.fn.filereadable(file) == 1 and vim.fs.root(file, ".git")
    root = root and (vim.uv.fs_realpath(root) or root)
    if root and not seen[root] then
      seen[root] = true
      recent = recent + 1
      table.insert(out, { kind = "recent", name = vim.fs.basename(root), path = root })
    end
  end
  table.insert(out, { kind = "browse", name = "browse for a directory…" })
  return out
end

-- label is how a candidate shows in the picker; the index in front maps it back
local function label(i, c)
  local icon = ({ pinned = icons.pinned, project = icons.unpinned, recent = icons.recent, browse = icons.project })[c.kind]
  local where = c.path and ("  " .. vim.fn.fnamemodify(c.path, ":~")) or ""
  return ("%d\t%s %s%s"):format(i, icon, c.name, where)
end

-- go opens a project: registers it (or bumps when it was last opened) through
-- the CLI, which owns projects.json, changes to it, then restores its session
-- when sessions are on and it has one, else an empty buffer with the layout
-- around it (JGA-161); that replaces the dashboard, so its keys go with it
function M.go(path)
  path = vim.fs.normalize(vim.fn.expand(path))
  if vim.fn.isdirectory(path) == 0 then
    vim.notify("bud: " .. path .. " isn't a directory", vim.log.levels.WARN)
    return
  end
  local bin = cli.bin()
  if bin then
    vim.system({ bin, "projects", "add", "--opened", path })
  end
  vim.cmd.cd(vim.fn.fnameescape(path))
  require("bud.ai.claude").opened(path)

  vim.cmd.enew()
  -- the saved session, else the layout (D39, JGA-74)
  require("bud.layout").open_project(path)
end

-- dirs is what the directory browser lists for dir: ./ and ../, then its
-- subdirectories, dot dirs too, sorted. Each is { name, path }
function M.dirs(dir)
  local out = { { name = "./", path = dir }, { name = "../", path = vim.fs.dirname(dir) } }
  local names = {}
  for name, kind in vim.fs.dir(dir) do
    local path = vim.fs.joinpath(dir, name)
    if kind == "directory" or (kind == "link" and vim.fn.isdirectory(path) == 1) then
      table.insert(names, name)
    end
  end
  table.sort(names)
  for _, name in ipairs(names) do
    table.insert(out, { name = name .. "/", path = vim.fs.joinpath(dir, name) })
  end
  return out
end

M.browse_keys = "<CR> go in · <S-CR> or o open as project · q cancel"

-- browse is the "browse for a directory…" entry (JGA-160): a float listing
-- only directories, netrw style. <CR> goes into the one under the cursor,
-- <S-CR> (or o, where the terminal can't tell it from <CR>) opens it as a
-- project. It starts in start, else the cwd
function M.browse(start)
  local ui = require("bud.ui")
  local dir = vim.fs.normalize(start or vim.fn.getcwd())
  local list = {}
  local h = ui.float({
    title = dir,
    icon = icons.project,
    footer = M.browse_keys,
    width = 64,
    height = 20,
    cursorline = true,
    wrap = false,
    filetype = "bud-browse",
  })

  -- show lists path, with the cursor on the entry named at (the dir just
  -- left, going up), else the first
  local function show(path, at)
    dir, list = path, M.dirs(path)
    local lines, row = {}, 1
    for i, d in ipairs(list) do
      table.insert(lines, " " .. d.name)
      if d.name == at then
        row = i
      end
    end
    vim.bo[h.buf].modifiable = true
    vim.api.nvim_buf_set_lines(h.buf, 0, -1, false, lines)
    vim.bo[h.buf].modifiable = false
    h.popup.border:set_text("top", " " .. icons.project .. " " .. vim.fn.fnamemodify(dir, ":~") .. " ", "center")
    vim.api.nvim_win_set_cursor(h.win, { row, 0 })
  end

  local function under()
    return list[vim.api.nvim_win_get_cursor(h.win)[1]]
  end
  h.map("<CR>", function()
    local d = under()
    if d and d.path ~= dir then
      show(d.path, d.name == "../" and vim.fs.basename(dir) .. "/" or nil)
    end
  end)
  local function pick()
    local d = under()
    if d then
      h.close()
      M.go(d.path)
    end
  end
  h.map("<S-CR>", pick)
  h.map("o", pick)
  show(dir)
  return h
end

-- project is p / <leader>p
function M.project()
  local list = M.candidates()
  local labels = {}
  for i, c in ipairs(list) do
    table.insert(labels, label(i, c))
  end
  fzf().fzf_exec(labels, {
    prompt = "Project> ",
    fzf_opts = { ["--delimiter"] = "\t", ["--with-nth"] = "2..", ["--no-sort"] = true },
    actions = {
      default = function(selected)
        local c = selected[1] and list[tonumber(selected[1]:match("^(%d+)\t"))]
        if not c then
          return
        end
        if c.kind == "browse" then
          M.browse()
        else
          M.go(c.path)
        end
      end,
    },
  })
end

-- root is the project the buffer's file belongs to: the registered project
-- holding it, else its git repo, else the cwd
function M.root(buf)
  local file = vim.api.nvim_buf_get_name(buf or 0)
  local dir = file ~= "" and vim.fs.dirname(file) or vim.fn.getcwd()
  local registered = projects.find(dir)
  if registered then
    return registered.path
  end
  return vim.fs.root(dir, ".git") or vim.fn.getcwd()
end

-- typed_path returns the file a query names when it's an existing absolute
-- (or ~) path, so f opens it even though it isn't in the project
function M.typed_path(query)
  if type(query) ~= "string" or not query:match("^[/~]") then
    return nil
  end
  local path = vim.fn.expand(vim.trim(query))
  if vim.fn.filereadable(path) == 1 then
    return path
  end
  return nil
end

-- file is f / <leader>f: recent files on the dashboard, files in the current
-- project anywhere else (opts.dir to pick another dir); typing an absolute
-- path that exists opens it
function M.file(opts)
  opts = opts or {}
  local actions = fzf().actions
  local function edit(selected, o)
    local path = M.typed_path(o.last_query)
    if path then
      vim.cmd.edit(vim.fn.fnameescape(path))
    elseif #selected > 0 then
      actions.file_edit_or_qf(selected, o)
    end
  end

  if not opts.dir and vim.bo.filetype == "bud-dashboard" then
    fzf().oldfiles({ prompt = "Recent> ", actions = { default = edit } })
    return
  end
  fzf().files({ cwd = opts.dir or M.root(), prompt = "Files> ", actions = { default = edit } })
end

return M
