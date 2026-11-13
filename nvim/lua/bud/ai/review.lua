-- The diff review (BASE.md 4.2, D134): a proposed version of a file shown
-- beside the file as it is, in a tab of its own, to accept (ga, :w, ZZ, :x)
-- or reject (q, or closing it), with your own edits to it kept. Claude
-- Code's IDE bridge (openDiff) and every ACP backend's permission requests
-- (bud.ai.permissions) open edits here.

local M = {}

local diffs = {}

-- numbers each review's autocmd group: a replacement under the same name
-- gets its own, so the old one's late close can't delete the new one's
local reviews = 0

-- close_diff takes a review's tab and buffers away, once
local function close_diff(d)
  if d.closed then
    return
  end
  d.closed = true
  if diffs[d.name] == d then
    diffs[d.name] = nil
  end
  -- first, so closing its windows here doesn't count as closing it
  pcall(vim.api.nvim_del_augroup_by_id, d.group)
  if d.proposed and vim.api.nvim_buf_is_valid(d.proposed) then
    vim.bo[d.proposed].modified = false
  end
  if vim.api.nvim_tabpage_is_valid(d.tab) then
    if #vim.api.nvim_list_tabpages() > 1 then
      -- by number, so the tab you're on stays yours (a queued review may
      -- have opened by now)
      pcall(vim.cmd, "tabclose! " .. vim.api.nvim_tabpage_get_number(d.tab))
    else
      -- the only tab left (:tabonly): the file's window stays, out of diff mode
      for _, win in ipairs(vim.api.nvim_tabpage_list_wins(d.tab)) do
        vim.api.nvim_win_call(win, function()
          vim.cmd("diffoff")
        end)
      end
    end
  end
  for _, buf in ipairs({ d.proposed, d.scratch_old }) do
    if buf and vim.api.nvim_buf_is_valid(buf) then
      vim.api.nvim_buf_delete(buf, { force = true })
    end
  end
end

-- respond_review tells the review's caller once: yes, with the proposal as it now
-- stands (you may have edited it), or no. A yes only ever comes from the
-- proposal's own keys and :w, so its buffer is there to read
local function respond_review(d, accepted)
  if d.done then
    return
  end
  d.done = true
  local proposal = ""
  if accepted then
    proposal = table.concat(vim.api.nvim_buf_get_lines(d.proposed, 0, -1, false), "\n") .. (d.eol and "\n" or "")
  end
  d.on_done(accepted, proposal)
end

-- finish answers a review and closes it
local function finish(d, accepted)
  respond_review(d, accepted)
  close_diff(d)
end

-- close_later closes a review on the next tick, when it was answered inside a
-- command that's still going: the quit in :wq, a window closing. Closing the
-- tab under :wq would hand its quit to one of your own windows
local function close_later(d)
  vim.schedule(function()
    close_diff(d)
  end)
end

-- review shows a proposed version of a file as a diff in its own tab and
-- calls on_done(accepted, text) once you decide: :w or ga accepts, q or
-- closing it rejects. opts: path (the file now; may not exist yet),
-- contents (the proposal), name (one review per name), title (the winbar)
function M.review(opts, on_done)
  local old, name = opts.path, opts.name or "proposed"
  if diffs[name] then
    finish(diffs[name], false)
  end
  local contents = opts.contents or ""
  reviews = reviews + 1
  local d = { name = name, on_done = on_done, eol = contents:sub(-1) == "\n" }
  d.group = vim.api.nvim_create_augroup("bud.review." .. reviews, {})

  vim.cmd.tabnew()
  d.tab = vim.api.nvim_get_current_tabpage()
  -- the file as it is: its buffer when it's open (unsaved changes and all), else from disk
  if old and vim.fn.filereadable(old) == 1 then
    vim.cmd.edit(vim.fn.fnameescape(old))
  else
    d.scratch_old = vim.api.nvim_get_current_buf()
    vim.bo[d.scratch_old].buftype = "nofile"
    vim.api.nvim_buf_set_name(d.scratch_old, "bud://" .. name .. " (new file)")
  end
  local old_win = vim.api.nvim_get_current_win()
  vim.cmd.diffthis()

  vim.cmd("rightbelow vnew")
  d.proposed = vim.api.nvim_get_current_buf()
  local body = contents:gsub("\n$", "")
  vim.api.nvim_buf_set_lines(d.proposed, 0, -1, false, vim.split(body, "\n", { plain = true }))
  vim.bo[d.proposed].buftype = "acwrite"
  vim.bo[d.proposed].bufhidden = "wipe"
  -- a big file's proposed side gets its real filetype's syntax and nothing
  -- else, as the file itself does (D94)
  local bigfile = require("bud.bigfile")
  local orig = vim.api.nvim_win_get_buf(old_win)
  if bigfile.is(orig) then
    local proposed, ft = d.proposed, bigfile.filetype(orig)
    vim.schedule(function()
      if vim.api.nvim_buf_is_valid(proposed) then
        vim.bo[proposed].syntax = ft
      end
    end)
  else
    vim.bo[d.proposed].filetype = vim.bo[orig].filetype
  end
  vim.bo[d.proposed].modified = false
  pcall(vim.api.nvim_buf_set_name, d.proposed, "bud://" .. name .. " (proposed) " .. vim.fs.basename(old or name))
  vim.cmd.diffthis()
  vim.wo.winbar = "%#DiffAdd# "
    .. (opts.title or "The proposed edit")
    .. " %* ga or :w accept   q (or closing it) reject   (you can edit it first)"

  local function map(lhs, accepted, desc)
    vim.keymap.set("n", lhs, function()
      finish(d, accepted)
    end, { buffer = d.proposed, nowait = true, desc = desc })
  end
  map("ga", true, "Accept the edit")
  -- ZZ and :x mean write and close: here, accept, even with nothing edited
  -- (where they'd otherwise skip the write and just close, a no)
  map("ZZ", true, "Accept the edit")
  vim.keymap.set("ca", "x", function()
    return (vim.fn.getcmdtype() == ":" and vim.fn.getcmdline() == "x") and "wq" or "x"
  end, { buffer = d.proposed, expr = true })
  map("q", false, "Reject the edit")
  vim.api.nvim_create_autocmd("BufWriteCmd", {
    group = d.group,
    buffer = d.proposed,
    callback = function()
      -- only a write from the review itself accepts: :wa from your own tab
      -- leaves the proposal unsaved, so :wqa stops at it instead
      if vim.api.nvim_get_current_tabpage() ~= d.tab then
        return
      end
      respond_review(d, true)
      -- written, as far as :wq is concerned
      vim.bo[d.proposed].modified = false
      close_later(d)
    end,
  })
  -- closing it any other way is a no, said at once: either side's window
  -- (:q, <C-w>o, :tabclose) or the proposal itself going
  local function closed()
    respond_review(d, false)
    close_later(d)
  end
  vim.api.nvim_create_autocmd("WinClosed", {
    group = d.group,
    pattern = { tostring(old_win), tostring(vim.api.nvim_get_current_win()) },
    callback = closed,
  })
  vim.api.nvim_create_autocmd("BufWipeout", { group = d.group, buffer = d.proposed, callback = closed })
  diffs[name] = d
end

-- close closes the review named name: a no when it's still waiting
function M.close(name)
  local d = diffs[name]
  if d then
    if d.done then
      close_diff(d)
    else
      finish(d, false)
    end
  end
end

-- close_all says no to every open review and returns how many there were
function M.close_all()
  local n = 0
  for _, d in pairs(diffs) do
    n = n + 1
    finish(d, false)
  end
  return n
end

-- open is the reviews waiting for an answer, by name
function M.open()
  return diffs
end

return M
