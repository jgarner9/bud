-- The editor tools Claude Code calls over the IDE bridge (the protocol the
-- VS Code extension and claudecode.nvim speak): what's open, the selection,
-- diagnostics, and proposed edits as diffs to accept or reject.
--
-- Each tool is { description, schema, run = fn(args, respond) }. run calls
-- respond(content) once, now or later (openDiff waits for you); content is a
-- string or a list of strings, sent as MCP text content.

local M = {}

-- the latest selection in a file buffer, kept by the bridge
M.latest = nil

local function text(v)
  return type(v) == "string" and v or vim.json.encode(v)
end

-- real resolves symlinks (macOS's /var is /private/var), so a path Claude
-- Code sends matches the buffer whichever way either was spelled
local function real(path)
  path = vim.fs.normalize(path)
  return vim.uv.fs_realpath(path) or path
end

local function file_buf(path)
  if not path or path == "" then
    return nil
  end
  path = real(path)
  for _, buf in ipairs(vim.api.nvim_list_bufs()) do
    local name = vim.api.nvim_buf_get_name(buf)
    if vim.api.nvim_buf_is_loaded(buf) and name ~= "" and real(name) == path then
      return buf
    end
  end
end

local function uri(path)
  return vim.uri_from_fname(path)
end

-- a file window in the current tab to open things in, rather than a pane
local function editor_win()
  local cur = vim.api.nvim_get_current_win()
  if vim.bo[vim.api.nvim_win_get_buf(cur)].buftype == "" then
    return cur
  end
  for _, win in ipairs(vim.api.nvim_tabpage_list_wins(0)) do
    local buf = vim.api.nvim_win_get_buf(win)
    if vim.bo[buf].buftype == "" and vim.api.nvim_win_get_config(win).relative == "" then
      return win
    end
  end
  return cur
end

-- selection reads the visual selection, or the cursor as an empty one, in
-- win (default: the current window). Lines and characters are 0-based
function M.selection(win)
  win = win or vim.api.nvim_get_current_win()
  local buf = vim.api.nvim_win_get_buf(win)
  local name = vim.api.nvim_buf_get_name(buf)
  if vim.bo[buf].buftype ~= "" or name == "" then
    return nil
  end
  local mode = vim.api.nvim_get_mode().mode
  local cursor = vim.api.nvim_win_get_cursor(win)
  local sel = {
    filePath = name,
    fileUrl = uri(name),
    text = "",
    selection = {
      start = { line = cursor[1] - 1, character = cursor[2] },
      ["end"] = { line = cursor[1] - 1, character = cursor[2] },
      isEmpty = true,
    },
  }
  if win == vim.api.nvim_get_current_win() and (mode == "v" or mode == "V" or mode == "\22") then
    local s, e = vim.fn.getpos("v"), vim.fn.getpos(".")
    if s[2] > e[2] or (s[2] == e[2] and s[3] > e[3]) then
      s, e = e, s
    end
    local lines = vim.fn.getregion(s, e, { type = mode })
    local last = mode == "V" and #(vim.api.nvim_buf_get_lines(buf, e[2] - 1, e[2], false)[1] or "") or e[3]
    sel.text = table.concat(lines, "\n")
    sel.selection = {
      start = { line = s[2] - 1, character = mode == "V" and 0 or s[3] - 1 },
      ["end"] = { line = e[2] - 1, character = last },
      isEmpty = false,
    }
  end
  return sel
end

-- diffs open for review, by tab name
local diffs = {}

local function close_diff(d)
  diffs[d.name] = nil
  if d.proposed and vim.api.nvim_buf_is_valid(d.proposed) then
    vim.bo[d.proposed].modified = false
  end
  if vim.api.nvim_tabpage_is_valid(d.tab) and #vim.api.nvim_list_tabpages() > 1 then
    vim.api.nvim_set_current_tabpage(d.tab)
    pcall(vim.cmd, "tabclose!")
  end
  for _, buf in ipairs({ d.proposed, d.scratch_old }) do
    if buf and vim.api.nvim_buf_is_valid(buf) then
      vim.api.nvim_buf_delete(buf, { force = true })
    end
  end
end

-- finish answers a diff once: accepted sends the proposal as it now stands
-- (you may have edited it), which Claude Code then writes itself
local function finish(d, accepted)
  if d.done then
    return
  end
  d.done = true
  local reply
  if accepted then
    local lines = vim.api.nvim_buf_get_lines(d.proposed, 0, -1, false)
    reply = { "FILE_SAVED", table.concat(lines, "\n") .. (d.eol and "\n" or "") }
  else
    reply = { "DIFF_REJECTED", d.name }
  end
  -- answered first, so nothing going wrong closing the tab can lose it
  d.respond(reply)
  close_diff(d)
  if accepted then
    -- pick up the file once Claude Code has written it
    vim.defer_fn(function()
      pcall(vim.cmd.checktime)
    end, 500)
  end
end

local function open_diff(args, respond)
  local old, new, name = args.old_file_path, args.new_file_path or args.old_file_path, args.tab_name or "proposed"
  if diffs[name] then
    finish(diffs[name], false)
  end
  local contents = args.new_file_contents or ""
  local d = { name = name, respond = respond, eol = contents:sub(-1) == "\n" }

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
  vim.bo[d.proposed].filetype = vim.bo[vim.api.nvim_win_get_buf(old_win)].filetype
  vim.bo[d.proposed].modified = false
  pcall(vim.api.nvim_buf_set_name, d.proposed, "bud://" .. name .. " (proposed) " .. vim.fs.basename(new))
  vim.cmd.diffthis()
  vim.wo.winbar = "%#DiffAdd# Claude's edit %* :w or ga accept   q reject   (you can edit it first)"

  local function map(lhs, accepted, desc)
    vim.keymap.set("n", lhs, function()
      finish(d, accepted)
    end, { buffer = d.proposed, nowait = true, desc = desc })
  end
  map("ga", true, "Accept Claude's edit")
  map("q", false, "Reject Claude's edit")
  vim.api.nvim_create_autocmd("BufWriteCmd", {
    buffer = d.proposed,
    callback = function()
      finish(d, true)
    end,
  })
  -- closing it any other way is a no
  vim.api.nvim_create_autocmd("BufWipeout", {
    buffer = d.proposed,
    callback = function()
      vim.schedule(function()
        finish(d, false)
      end)
    end,
  })
  diffs[name] = d
end

local severities = { "Error", "Warning", "Information", "Hint" }

local function diagnostics_for(buf)
  local out = {}
  for _, dg in ipairs(vim.diagnostic.get(buf)) do
    table.insert(out, {
      message = dg.message,
      severity = severities[dg.severity] or "Error",
      source = dg.source,
      code = dg.code and tostring(dg.code) or nil,
      range = {
        start = { line = dg.lnum, character = dg.col },
        ["end"] = { line = dg.end_lnum or dg.lnum, character = dg.end_col or dg.col },
      },
    })
  end
  return out
end

local no_args = { type = "object", properties = vim.empty_dict() }

M.tools = {
  openFile = {
    description = "Open a file in the editor, optionally selecting a range of text",
    schema = {
      type = "object",
      properties = {
        filePath = { type = "string", description = "Path of the file to open" },
        preview = { type = "boolean" },
        startText = { type = "string", description = "Text marking the start of the selection" },
        endText = { type = "string", description = "Text marking the end of the selection" },
        selectToEndOfLine = { type = "boolean" },
        makeFrontmost = { type = "boolean" },
      },
      required = { "filePath" },
    },
    run = function(args, respond)
      local path = vim.fn.fnamemodify(args.filePath, ":p")
      vim.api.nvim_set_current_win(editor_win())
      vim.cmd.edit(vim.fn.fnameescape(path))
      if args.startText and args.startText ~= "" then
        local lines = vim.api.nvim_buf_get_lines(0, 0, -1, false)
        for i, line in ipairs(lines) do
          local col = line:find(args.startText, 1, true)
          if col then
            vim.api.nvim_win_set_cursor(0, { i, col - 1 })
            break
          end
        end
      end
      if args.makeFrontmost == false then
        respond({
          success = true,
          filePath = path,
          languageId = vim.bo.filetype,
          lineCount = vim.api.nvim_buf_line_count(0),
        })
      else
        respond("Opened file: " .. path)
      end
    end,
  },

  openDiff = {
    description = "Show a proposed edit as a diff and wait for the user to accept (FILE_SAVED) or reject (DIFF_REJECTED) it",
    schema = {
      type = "object",
      properties = {
        old_file_path = { type = "string" },
        new_file_path = { type = "string" },
        new_file_contents = { type = "string" },
        tab_name = { type = "string" },
      },
      required = { "old_file_path", "new_file_path", "new_file_contents", "tab_name" },
    },
    run = open_diff,
  },

  getCurrentSelection = {
    description = "The current selection in the active editor",
    schema = no_args,
    run = function(_, respond)
      local sel = M.selection(editor_win())
      if not sel then
        return respond({ success = false, message = "No active editor found" })
      end
      respond(vim.tbl_extend("force", { success = true }, sel))
    end,
  },

  getLatestSelection = {
    description = "The most recent selection in any file",
    schema = no_args,
    run = function(_, respond)
      if not M.latest then
        return respond({ success = false, message = "No selection available" })
      end
      respond(vim.tbl_extend("force", { success = true }, M.latest))
    end,
  },

  getOpenEditors = {
    description = "The files open in the editor",
    schema = no_args,
    run = function(_, respond)
      local current = vim.api.nvim_win_get_buf(editor_win())
      local tabs = {}
      for _, buf in ipairs(vim.api.nvim_list_bufs()) do
        local name = vim.api.nvim_buf_get_name(buf)
        if vim.bo[buf].buflisted and vim.bo[buf].buftype == "" and name ~= "" then
          table.insert(tabs, {
            uri = uri(name),
            isActive = buf == current,
            label = vim.fs.basename(name),
            languageId = vim.bo[buf].filetype,
            isDirty = vim.bo[buf].modified,
          })
        end
      end
      respond({ tabs = tabs })
    end,
  },

  getWorkspaceFolders = {
    description = "The workspace folders open in the editor",
    schema = no_args,
    run = function(_, respond)
      local folders = {}
      for _, path in ipairs(require("bud.ai.bridge").folders()) do
        table.insert(folders, { name = vim.fs.basename(path), uri = uri(path), path = path })
      end
      respond({ success = true, folders = folders, rootPath = folders[1] and folders[1].path or vim.fn.getcwd() })
    end,
  },

  getDiagnostics = {
    description = "Diagnostics (errors, warnings) for one file, or every open one",
    schema = { type = "object", properties = { uri = { type = "string" } } },
    run = function(args, respond)
      local out = {}
      if args.uri and args.uri ~= "" then
        local buf = file_buf(vim.uri_to_fname(args.uri))
        table.insert(out, { uri = args.uri, diagnostics = buf and diagnostics_for(buf) or {} })
      else
        for _, buf in ipairs(vim.api.nvim_list_bufs()) do
          local name = vim.api.nvim_buf_get_name(buf)
          local found = vim.api.nvim_buf_is_loaded(buf) and name ~= "" and diagnostics_for(buf) or {}
          if #found > 0 then
            table.insert(out, { uri = uri(name), diagnostics = found })
          end
        end
      end
      respond(out)
    end,
  },

  checkDocumentDirty = {
    description = "Whether a file has unsaved changes in the editor",
    schema = { type = "object", properties = { filePath = { type = "string" } }, required = { "filePath" } },
    run = function(args, respond)
      local buf = file_buf(args.filePath)
      if not buf then
        return respond({ success = false, message = "Document not open: " .. tostring(args.filePath) })
      end
      respond({ success = true, filePath = args.filePath, isDirty = vim.bo[buf].modified, isUntitled = false })
    end,
  },

  saveDocument = {
    description = "Save a file open in the editor",
    schema = { type = "object", properties = { filePath = { type = "string" } }, required = { "filePath" } },
    run = function(args, respond)
      local buf = file_buf(args.filePath)
      if not buf then
        return respond({ success = false, message = "Document not open: " .. tostring(args.filePath) })
      end
      vim.api.nvim_buf_call(buf, function()
        vim.cmd.write()
      end)
      respond({ success = true, filePath = args.filePath, saved = true, message = "Document saved successfully" })
    end,
  },

  close_tab = {
    description = "Close a diff tab by name",
    schema = { type = "object", properties = { tab_name = { type = "string" } }, required = { "tab_name" } },
    run = function(args, respond)
      local d = diffs[args.tab_name]
      if d then
        if d.done then
          close_diff(d)
        else
          finish(d, false)
        end
      end
      respond("TAB_CLOSED")
    end,
  },

  closeAllDiffTabs = {
    description = "Close every diff tab",
    schema = no_args,
    run = function(_, respond)
      local n = 0
      for _, d in pairs(diffs) do
        n = n + 1
        finish(d, false)
      end
      respond("CLOSED_" .. n .. "_DIFF_TABS")
    end,
  },
}

-- content turns a tool's answer into MCP text content
function M.content(answer)
  local parts = type(answer) == "table"
      and vim.islist(answer)
      and #answer > 0
      and type(answer[1]) == "string"
      and answer
    or { answer }
  return vim.tbl_map(function(p)
    return { type = "text", text = text(p) }
  end, parts)
end

-- open_diffs is the diffs waiting for an answer, for tests
function M.open_diffs()
  return diffs
end

return M
