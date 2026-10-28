-- Claude Code's permission questions as Neovim UI (BASE.md 4.2, JGA-82). A
-- headless session runs with --permission-prompt-tool pointed at the
-- bridge's approve tool, so each "may I?" comes here: an edit opens as a diff
-- to accept or reject (bud.ai.bridge.tools.review), anything else as a small
-- prompt. bud never changes Claude Code's permission mode; "allow for this
-- session" is only remembered here, for the session's Neovim lifetime.

local M = {}

-- the tools whose input is a change to one file, shown as a diff
local edit_tools = { Edit = true, MultiEdit = true, Write = true }

-- allowed for the rest of the session, by root: { [key] = true }
local remembered = {}
local queue = {}
local showing

local function read(path)
  local buf = vim.fn.bufnr(path)
  if buf ~= -1 and vim.api.nvim_buf_is_loaded(buf) then
    return table.concat(vim.api.nvim_buf_get_lines(buf, 0, -1, false), "\n") .. "\n"
  end
  local fd = io.open(path, "r")
  if not fd then
    return nil
  end
  local text = fd:read("*a")
  fd:close()
  return text
end

local function replace(text, old, new, all)
  local out, start, n = {}, 1, 0
  while true do
    local i, j = text:find(old, start, true)
    if not i then
      break
    end
    table.insert(out, text:sub(start, i - 1) .. new)
    start, n = j + 1, n + 1
    if not all then
      break
    end
  end
  table.insert(out, text:sub(start))
  return table.concat(out), n
end

-- proposal works out a file edit's result: the file's path, its text now and
-- after, or nil when the edit can't be applied (Claude Code will say why)
function M.proposal(tool, input)
  local path = input.file_path
  if not path then
    return nil
  end
  local now = read(path)
  if tool == "Write" then
    return path, now, input.content or ""
  end
  if not now then
    return nil
  end
  local edits = tool == "MultiEdit" and input.edits or { input }
  local after = now
  for _, e in ipairs(edits or {}) do
    local n
    after, n = replace(after, e.old_string or "", e.new_string or "", e.replace_all)
    if n == 0 then
      return nil
    end
  end
  return path, now, after
end

-- edited is the tool input that makes the file what you ended up with, in
-- the same tool's shape (Claude Code checks it against that tool's schema):
-- the whole file replaced, which is unique by definition
function M.edited(tool, path, now, text)
  if tool == "Write" or not now then
    return { file_path = path, content = text }
  elseif tool == "MultiEdit" then
    return { file_path = path, edits = { { old_string = now, new_string = text } } }
  end
  return { file_path = path, old_string = now, new_string = text }
end

-- key is what "allow for this session" remembers: the exact command for
-- Bash, else the tool
function M.key(tool, input)
  if tool == "Bash" then
    return "Bash:" .. tostring(input.command)
  end
  return tool
end

-- summary is one line saying what the tool wants to do
function M.summary(_, input)
  local detail = input.command or input.file_path or input.url or input.pattern or input.path or input.query
  if not detail then
    local ok, json = pcall(vim.json.encode, input)
    detail = ok and json or ""
  end
  detail = tostring(detail):gsub("\n", " ")
  if #detail > 200 then
    detail = detail:sub(1, 197) .. "..."
  end
  return detail
end

local function allow(input)
  return { behavior = "allow", updatedInput = input }
end

local function deny(message)
  return { behavior = "deny", message = message or "The user said no." }
end

local show_next

-- prompt shows one non-edit question in a dialog (bud.ui) and answers
-- through done; closing it any other way is a no, so Claude Code never waits
local function prompt(req, done)
  local lines = {
    "",
    "  [o.o]  Claude Code wants to use " .. req.tool,
    "",
    "    " .. M.summary(req.tool, req.input),
    "",
  }
  if req.root then
    table.insert(lines, 3, "         in " .. vim.fn.fnamemodify(req.root, ":~"))
  end
  require("bud.ui").dialog({
    title = "permission",
    lines = lines,
    actions = {
      {
        key = "y",
        label = "allow",
        fn = function()
          done(allow(req.input))
        end,
      },
      {
        key = "a",
        label = "allow for this session",
        fn = function()
          remembered[req.root or ""] = remembered[req.root or ""] or {}
          remembered[req.root or ""][M.key(req.tool, req.input)] = true
          done(allow(req.input))
        end,
      },
      {
        key = { "n", "q", "<Esc>" },
        label = "deny",
        fn = function()
          done(deny())
        end,
      },
      {
        key = "r",
        label = "deny and say why",
        fn = function()
          vim.ui.input({ prompt = "Tell Claude why not: " }, function(why)
            done(deny(why and why ~= "" and ("The user said no: " .. why) or nil))
          end)
        end,
      },
    },
    cancel = function()
      done(deny())
    end,
  })
end

local function ask(req, done)
  if edit_tools[req.tool] then
    local path, now, after = M.proposal(req.tool, req.input)
    if path then
      require("bud.ai.bridge.tools").review({
        path = path,
        contents = after,
        name = "permission-" .. (req.id or path),
        title = "Claude wants to " .. (req.tool == "Write" and "write" or "edit") .. " " .. vim.fn.fnamemodify(
          path,
          ":~:."
        ),
      }, function(accepted, text)
        if not accepted then
          return done(deny("The user rejected this edit."))
        end
        if text == after then
          return done(allow(req.input))
        end
        done(allow(M.edited(req.tool, path, now, text)))
      end)
      return
    end
  end
  prompt(req, done)
end

show_next = function()
  if showing or #queue == 0 then
    return
  end
  showing = table.remove(queue, 1)
  local req = showing
  ask(req, function(result)
    showing = nil
    req.respond(result)
    vim.schedule(show_next)
  end)
end

-- policies answer some requests without asking: each is fn(req) returning
-- "allow", "deny" (and a message for Claude), or nil to pass. A module adds
-- one for the requests it owns, such as bud.ai.issue's Linear writes
M.policies = {}

-- request asks about one tool use: req = { tool, input, root, id,
-- respond(result) }, where result is Claude Code's { behavior, ... }.
-- Requests wait their turn, one on screen at a time
function M.request(req)
  for _, policy in ipairs(M.policies) do
    local verdict, message = policy(req)
    if verdict == "allow" then
      return req.respond(allow(req.input))
    elseif verdict == "deny" then
      return req.respond(deny(message))
    end
  end
  local mine = remembered[req.root or ""]
  if mine and mine[M.key(req.tool, req.input)] then
    return req.respond(allow(req.input))
  end
  require("bud.ai.claude")._emit("permission_request", { tool = req.tool, input = req.input }, req.root)
  table.insert(queue, req)
  show_next()
end

-- forget drops the session's "allow for this session" answers
function M.forget(root)
  remembered[root or ""] = nil
end

-- pending is how many questions are waiting, the one on screen included
function M.pending()
  return #queue + (showing and 1 or 0)
end

return M
