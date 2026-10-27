-- Pinned files: four slots per project (BASE.md 3, D29). Slots are stored in
-- marks.json in bud's state dir, keyed by project root, with paths relative to
-- that root so they read the same on every machine.

local M = {}

M.slots = 4

-- tests point this at a temp file
M.file = vim.fn.stdpath("state") .. "/marks.json"

local function read()
  local f = io.open(M.file, "r")
  if not f then
    return {}
  end
  local data = f:read("*a")
  f:close()
  local ok, decoded = pcall(vim.json.decode, data)
  if not ok or type(decoded) ~= "table" then
    vim.notify("bud: ignoring unreadable " .. M.file, vim.log.levels.WARN)
    return {}
  end
  return decoded
end

-- write to a temp file and rename, so a crash never leaves half a file
local function write(all)
  vim.fn.mkdir(vim.fs.dirname(M.file), "p")
  local tmp = M.file .. ".tmp"
  local f = assert(io.open(tmp, "w"))
  f:write(vim.json.encode(all))
  f:close()
  assert(vim.uv.fs_rename(tmp, M.file))
end

-- real resolves symlinks, so a project opened through one (or macOS's /tmp ->
-- /private/tmp) matches the paths of its files. A file that doesn't exist yet
-- is resolved through its directory
local function real(path)
  path = vim.fs.normalize(path)
  local resolved = vim.uv.fs_realpath(path)
  if resolved then
    return resolved
  end
  local dir = vim.uv.fs_realpath(vim.fs.dirname(path))
  return dir and (dir .. "/" .. vim.fs.basename(path)) or path
end

local function root()
  return real(vim.fn.getcwd())
end

-- get returns the project's slots as a table of slot number -> relative path
function M.get()
  local slots = {}
  for k, path in pairs(read()[root()] or {}) do
    slots[tonumber(k)] = path
  end
  return slots
end

local function save(slots)
  local all = read()
  local stored = {}
  for i, path in pairs(slots) do
    stored[tostring(i)] = path
  end
  all[root()] = next(stored) and stored or nil
  write(all)
end

-- relative returns path relative to the project root, or nil when it's outside it
local function relative(path)
  local base = root() .. "/"
  path = real(path)
  if path:sub(1, #base) ~= base then
    return nil
  end
  return path:sub(#base + 1)
end

-- pin puts path in slot, or in the first free one when slot is nil. It returns
-- the slot used, or nil and a reason
function M.pin(path, slot)
  local rel = relative(path)
  if not rel then
    return nil, "not inside the project"
  end
  local slots = M.get()
  for i = 1, M.slots do
    if slots[i] == rel then
      return nil, "already pinned to " .. i
    end
  end
  if not slot then
    for i = 1, M.slots do
      if not slots[i] then
        slot = i
        break
      end
    end
    if not slot then
      return nil, "all slots are taken"
    end
  end
  slots[slot] = rel
  save(slots)
  return slot
end

function M.unpin(slot)
  local slots = M.get()
  slots[slot] = nil
  save(slots)
end

-- path returns the absolute path in slot, or nil
function M.path(slot)
  local rel = M.get()[slot]
  return rel and (root() .. "/" .. rel) or nil
end

-- the commands behind the keymaps

function M.jump(slot)
  local path = M.path(slot)
  if not path then
    vim.notify("bud: slot " .. slot .. " is empty", vim.log.levels.INFO)
    return
  end
  vim.cmd.edit(vim.fn.fnameescape(path))
end

local function describe(slots)
  local items = {}
  for i = 1, M.slots do
    table.insert(items, { slot = i, label = ("%d  %s"):format(i, slots[i] or "(empty)") })
  end
  return items
end

function M.pin_current()
  local path = vim.api.nvim_buf_get_name(0)
  if path == "" then
    vim.notify("bud: this buffer has no file to pin", vim.log.levels.INFO)
    return
  end
  local slot, err = M.pin(path)
  if slot then
    vim.notify(("bud: pinned to %d"):format(slot))
  elseif err == "all slots are taken" then
    vim.ui.select(describe(M.get()), {
      prompt = "Replace which slot?",
      format_item = function(item)
        return item.label
      end,
    }, function(item)
      if item then
        M.unpin(item.slot)
        M.pin(path, item.slot)
      end
    end)
  else
    vim.notify("bud: " .. err, vim.log.levels.INFO)
  end
end

function M.unpin_menu()
  vim.ui.select(describe(M.get()), {
    prompt = "Unpin which slot?",
    format_item = function(item)
      return item.label
    end,
  }, function(item)
    if item then
      M.unpin(item.slot)
    end
  end)
end

function M.setup()
  local map = vim.keymap.set
  for i = 1, M.slots do
    map("n", "<leader>" .. i, function()
      M.jump(i)
    end, { desc = "Pinned file " .. i })
  end
  map("n", "<leader>m", M.pin_current, { desc = "Pin file" })
  map("n", "<leader>M", M.unpin_menu, { desc = "Unpin file" })
end

return M
