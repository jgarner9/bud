-- Run with: nvim --clean --headless -l tests/lua/view_test.lua (just lua-test)
-- bud's own chat (D138) on the fake ACP agent: streamed replies, tool calls
-- with their state, OpenClaw's after-the-fact patch, errors, the pane and
-- full screen, and the lock

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local function wait(cond, what)
  if not vim.wait(10000, cond, 10) then
    error("timed out waiting for " .. what, 2)
  end
end

local repo = vim.fn.getcwd()
local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp .. "/proj", "p")
local proj = vim.uv.fs_realpath(tmp .. "/proj")
require("bud.config").file = tmp .. "/config.json"
require("bud.lock").setup()

local agent = require("bud.ai.agent")
agent.command = function()
  return { vim.v.progpath, "--clean", "--headless", "-l", repo .. "/tests/lua/lib/fake_acp.lua" }
end
agent.permission = function(_, respond)
  respond("allow-once")
end
local view = require("bud.ai.view")
local editor = vim.api.nvim_get_current_win()

local function text()
  return table.concat(view.lines(proj), "\n")
end
local turns = 0
agent.on("done", function()
  turns = turns + 1
end)
local function turn(msg)
  local before = turns
  view.send(msg, proj)
  wait(function()
    return turns > before
  end, msg)
  vim.wait(50)
end

-- a pane on the right, the prompt under it, the cursor left alone
local win = view.open({ root = proj, focus = false })
eq(vim.api.nvim_get_current_win(), editor, "focus stays")
eq(view.showing(proj), true, "showing")
eq(#vim.api.nvim_tabpage_list_wins(0), 3, "editor, conversation, prompt")
eq(vim.bo[vim.api.nvim_win_get_buf(win)].filetype, "markdown", "markdown")
wait(function()
  return agent.status(proj) == "ready"
end, "the agent")
assert(vim.wo[win].winbar:find("openclaw · proj · ready", 1, true), vim.wo[win].winbar)

-- a streamed reply
turn("hello")
assert(text():find("## you\n\nhello\n\n## openclaw\n\nHello from the fake.", 1, true), text())

-- a tool call shows as one line with its state
vim.fn.writefile({ "old" }, proj .. "/f.txt")
turn("edit " .. proj .. "/f.txt")
assert(text():find("> ✓ Edit " .. proj .. "/f.txt", 1, true), text())

-- OpenClaw's edit: the patch it reports afterwards is shown (D134)
turn("replay " .. repo .. "/tests/fixtures/openclaw/edit.log")
assert(text():find("```diff\n--- notes.txt\n+++ notes.txt", 1, true), text())
assert(text():find("+gamma", 1, true), text())

-- thinking is one line, a plan a checklist
turn("think")
assert(text():find("> thinking…\n\n> plan:\n> - [x] look\n> - [ ] fix", 1, true), text())
eq(#vim.tbl_filter(function(l)
  return l == "> thinking…"
end, view.lines(proj)), 1, "one thinking line for two thoughts")
assert(text():find("Thought about it.", 1, true), text())

-- the backend's slash commands complete in the prompt
vim.cmd.cd(proj)
eq(
  vim.tbl_map(function(i)
    return i.word
  end, view.complete(0, "/st")),
  { "/status" },
  "slash commands"
)

-- an error says so
turn("fail")
assert(text():find("> ✗ scripted failure", 1, true), text())

-- locked: :close brings it back
vim.api.nvim_win_close(win, false)
wait(function()
  return view.showing(proj) and #vim.api.nvim_tabpage_list_wins(0) == 3
end, "the chat to reopen")

-- the toggle closes it, keeping the conversation
vim.cmd.cd(proj)
view.toggle()
eq(view.showing(proj), false, "closed")
eq(#vim.api.nvim_tabpage_list_wins(0), 1, "just the editor")
assert(text():find("Hello from the fake.", 1, true), "the conversation stays")

-- full screen: a tab of its own
local tabs = #vim.api.nvim_list_tabpages()
view.open({ root = proj, layout = "full", focus = false })
eq(#vim.api.nvim_list_tabpages(), tabs + 1, "a tab")
eq(#vim.api.nvim_tabpage_list_wins(0), 2, "conversation and prompt only")
view.close(proj)
eq(#vim.api.nvim_list_tabpages(), tabs, "the tab goes")

-- ai.agent.chat_ui = "bud": <leader>ac (term.toggle) and <leader>aF use it
vim.fn.writefile({ vim.json.encode({ ai = { agent = { chat_ui = "bud" } } }) }, tmp .. "/config.json")
local term = require("bud.ai.term")
term.toggle()
eq(view.showing(proj), true, "<leader>ac opened bud's chat")
vim.cmd.stopinsert()
term.full()
eq(#vim.api.nvim_list_tabpages(), tabs + 1, "<leader>aF made it full screen")
vim.cmd.stopinsert()
term.full()
eq(#vim.api.nvim_list_tabpages(), tabs, "and back to the pane")
eq(view.showing(proj), true, "still showing, as a pane")
vim.cmd.stopinsert()
term.toggle()
eq(view.showing(proj), false, "<leader>ac closed it")
require("bud.layout.panes")
eq(require("bud.layout").providers.chat.available(), true, "the layout's chat pane is available")

view._reset()
agent._reset()
vim.fn.delete(tmp, "rf")
print("view_test: ok")
