-- Run with: nvim --clean --headless -l tests/lua/chat_test.lua (just lua-test)
-- Full-screen chat, its history, and the side pane, against a fake claude that
-- keeps its conversations where Claude Code does

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local function wait(cond, what)
  if not vim.wait(5000, cond, 10) then
    error("timed out waiting for " .. what, 2)
  end
end

local tmp = vim.uv.fs_realpath(vim.fn.tempname()) or vim.fn.tempname()
vim.fn.mkdir(tmp, "p")
tmp = vim.uv.fs_realpath(tmp)
vim.env.CLAUDE_CONFIG_DIR = tmp .. "/claude"
vim.env.XDG_STATE_HOME = tmp .. "/state"
require("bud.config").file = tmp .. "/config.json"
require("bud.ai.bridge").lock_dir = function()
  return tmp .. "/ide"
end

-- the fake writes each exchange into $CLAUDE_CONFIG_DIR/projects/<dir>/<id>.jsonl
-- and answers "echo: <text>"; --resume picks the id, else it makes one
local fake = tmp .. "/fake-claude"
vim.fn.writefile({
  "#!/bin/sh",
  'id="s$$"',
  'while [ $# -gt 0 ]; do [ "$1" = --resume ] && id="$2"; shift; done',
  [[dir="$CLAUDE_CONFIG_DIR/projects/$(pwd -P | sed 's/[^a-zA-Z0-9]/-/g')"]],
  'mkdir -p "$dir"',
  "while IFS= read -r line; do",
  [[  text=$(printf '%s' "$line" | sed 's/.*"content":"\([^"]*\)".*/\1/')]],
  [[  printf '{"type":"user","message":{"role":"user","content":"%s"}}\n' "$text" >> "$dir/$id.jsonl"]],
  [[  printf '{"type":"assistant","message":{"content":[{"type":"text","text":"echo: %s"}]}}\n' "$text" >> "$dir/$id.jsonl"]],
  [[  printf '{"type":"system","subtype":"init","session_id":"%s","model":"claude-test-1"}\n' "$id"]],
  [[  printf '{"type":"assistant","session_id":"%s","message":{"content":[{"type":"text","text":"echo: %s"}]}}\n' "$id" "$text"]],
  [[  printf '{"type":"result","subtype":"success","session_id":"%s","result":"echo: %s"}\n' "$id" "$text"]],
  "done",
}, fake)
vim.fn.setfperm(fake, "rwxr-xr-x")

local claude = require("bud.ai.claude")
claude.bin = function()
  return fake
end
claude.secret_env = function(cb)
  cb({})
end
claude.check_login = function(_, cb)
  vim.schedule(function()
    cb(true)
  end)
end

local store = require("bud.chat.store")
local view = require("bud.chat.view")
local chat = require("bud.chat")

-- pure parts
eq(store.title("how do layouts work?\nmore"), "how do layouts work?", "title is the first line")
eq(vim.fn.strchars(store.title(string.rep("x", 80))), 50, "titles are short")
eq(store.day(os.time(), os.time()), "Today", "today")
eq(store.day(os.time() - 86400, os.time()), "Yesterday", "yesterday")
eq(store.project_dir("/a/b.c"):match("[^/]+$"), "-a-b-c", "Claude Code's dir naming")
local lines = view.render({
  { role = "user", text = "hi" },
  { role = "tool", name = "Read", input = { file_path = "x.go" } },
  { role = "assistant", text = "hello\nthere" },
})
eq(lines[1], "you", "you label")
eq(lines[3]:match("Read x.go") ~= nil, true, "tool line")
eq(vim.list_slice(lines, #lines - 1), { "hello", "there" }, "reply text")
local doc = { "text", "```go", "package main", "```", "after" }
eq({ view.code_block(doc, 3) }, { "go", { "package main" } }, "code block under the cursor")
eq(view.code_block(doc, 5), nil, "not in a block")

-- @file and @project turn into context
local file = tmp .. "/proj/main.go"
vim.fn.mkdir(tmp .. "/proj", "p")
vim.fn.writefile({ "package main" }, file)
vim.cmd.edit(file)
local editor = vim.api.nvim_get_current_win()
local text, context = view.expand("look at @file in @project", editor, tmp .. "/proj")
eq(text, "look at @main.go in the project", "@-words read naturally")
eq(context, { file, tmp .. "/proj/" }, "and become context")

-- full-screen chat: its own tab, a new conversation, Enter sends
chat.open()
local v = chat.view()
local tab = vim.api.nvim_get_current_tabpage()
eq(tab ~= vim.api.nvim_win_get_tabpage(editor), true, "its own tab")
eq(vim.api.nvim_get_current_buf(), v.input, "typing straight away")
vim.api.nvim_buf_set_lines(v.input, 0, -1, false, { "plan the roadmap" })
v:send()
wait(function()
  return vim.tbl_contains(vim.api.nvim_buf_get_lines(v.transcript, 0, -1, false), "echo: plan the roadmap")
end, "the reply")
eq(vim.api.nvim_buf_get_lines(v.input, 0, -1, false), { "" }, "input cleared")
eq(claude.get("chat").root, vim.uv.fs_realpath(store.chat_dir()), "runs in the chat dir")

-- history: the conversation is listed, renamed, pinned
wait(function()
  return #store.list() == 1
end, "the conversation in the store")
local entry = store.list()[1]
eq(entry.title, "plan the roadmap", "titled from the first message")
store.rename(entry.id, "bud roadmap")
store.pin(entry.id, true)
eq({ store.list()[1].title, store.list()[1].pinned }, { "bud roadmap", true }, "renamed and pinned")
local hl = chat.history_lines(store.list(), nil, os.time())
eq({ hl[1], hl[2]:match("bud roadmap") ~= nil }, { " Pinned", true }, "pinned group first")

-- a new chat, then resuming the first brings its transcript back
vim.api.nvim_set_current_win(v.win)
chat.history()
local hbuf = vim.api.nvim_get_current_buf()
vim.api.nvim_feedkeys("n", "x", false)
eq(vim.api.nvim_buf_get_lines(v.transcript, 1, 2, false)[1]:match("ask away") ~= nil, true, "a blank chat")
vim.api.nvim_buf_set_lines(v.input, 0, -1, false, { "second chat" })
v:send()
wait(function()
  return #store.list() == 2
end, "the second conversation")
vim.api.nvim_set_current_buf(hbuf)
vim.api.nvim_set_current_win(vim.fn.bufwinid(hbuf))
for i, l in ipairs(vim.api.nvim_buf_get_lines(hbuf, 0, -1, false)) do
  if l:match("bud roadmap") then
    vim.api.nvim_win_set_cursor(0, { i, 0 })
  end
end
vim.api.nvim_feedkeys("\r", "x", false)
wait(function()
  return claude.get("chat").session_id == entry.id
end, "resuming the first")
eq(
  vim.tbl_contains(vim.api.nvim_buf_get_lines(v.transcript, 0, -1, false), "echo: plan the roadmap"),
  true,
  "scrollback"
)
eq(v.title, "bud roadmap", "its title")

-- q goes back to the editor, <leader>c-style toggle comes back
vim.api.nvim_set_current_win(v.win)
chat.toggle()
eq(vim.api.nvim_get_current_tabpage() ~= tab, true, "back to the editor")
chat.toggle()
eq(vim.api.nvim_get_current_tabpage(), tab, "back to chat")

-- delete removes it from Claude Code's store
local second
for _, e in ipairs(store.list()) do
  if e.id ~= entry.id then
    second = e
  end
end
store.delete(second)
eq(#store.list(), 1, "deleted")
eq(vim.fn.filereadable(second.path), 0, "the jsonl is gone")

-- the side pane runs on the project's own session, beside the editor
vim.cmd.tabprevious()
vim.fn.chdir(tmp .. "/proj")
local pane = require("bud.chat.pane")
local pwin = pane.open({ focus = false })
local pv = pane.view()
eq(vim.api.nvim_win_get_buf(pwin), pv.transcript, "a pane")
eq(pv.key, vim.uv.fs_realpath(tmp .. "/proj"), "on the project's session")
vim.api.nvim_buf_set_lines(pv.input, 0, -1, false, { "what does main do?" })
pv:send()
wait(function()
  return vim.tbl_contains(vim.api.nvim_buf_get_lines(pv.transcript, 0, -1, false), "echo: what does main do?")
end, "the pane's reply")
pane.toggle()
eq(vim.api.nvim_win_is_valid(pwin), false, "hidden")
pane._reset()
local again = pane.open({ focus = false })
wait(function()
  return claude.get(vim.uv.fs_realpath(tmp .. "/proj")).session_id ~= nil
end, "the session id")
eq(
  vim.tbl_contains(
    vim.api.nvim_buf_get_lines(vim.api.nvim_win_get_buf(again), 0, -1, false),
    "echo: what does main do?"
  ),
  true,
  "reopening shows the scrollback"
)

pane._reset()
chat._reset()
claude._reset()
require("bud.ai.bridge").stop()
vim.fn.delete(tmp, "rf")
print("chat_test: ok")
