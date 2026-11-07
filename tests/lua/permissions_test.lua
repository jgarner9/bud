-- Run with: nvim --clean --headless -l tests/lua/permissions_test.lua (just lua-test)
-- Claude Code's permission questions: edits as diffs, the rest as a prompt

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")
dofile("tests/lua/lib/nui.lua")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local function wait(cond, what)
  if not vim.wait(3000, cond, 10) then
    error("timed out waiting for " .. what, 2)
  end
end

local perms = require("bud.ai.permissions")
local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp, "p")
local file = tmp .. "/a.txt"
vim.fn.writefile({ "one", "two", "two" }, file)

-- what an edit would make of the file
eq(
  { perms.proposal("Edit", { file_path = file, old_string = "one", new_string = "1" }) },
  { file, "one\ntwo\ntwo\n", "1\ntwo\ntwo\n" },
  "edit"
)
eq(
  select(3, perms.proposal("Edit", { file_path = file, old_string = "two", new_string = "2", replace_all = true })),
  "one\n2\n2\n",
  "replace all"
)
eq(
  select(
    3,
    perms.proposal("MultiEdit", {
      file_path = file,
      edits = { { old_string = "one", new_string = "1" }, { old_string = "two", new_string = "2" } },
    })
  ),
  "1\n2\ntwo\n",
  "multi edit, in order"
)
eq(select(3, perms.proposal("Write", { file_path = tmp .. "/new.txt", content = "hi\n" })), "hi\n", "write a new file")
eq(perms.proposal("Edit", { file_path = file, old_string = "nope", new_string = "x" }), nil, "an edit that can't apply")
eq(
  perms.edited("Edit", file, "a\n", "b\n"),
  { file_path = file, old_string = "a\n", new_string = "b\n" },
  "edited keeps Edit's shape"
)
eq(perms.key("Bash", { command = "ls" }), "Bash:ls", "Bash is remembered by command")

-- a prompt: y allows
local result
local function ask(tool, input, root)
  result = nil
  perms.request({
    tool = tool,
    input = input,
    root = root or tmp,
    respond = function(r)
      result = r
    end,
  })
end
ask("Bash", { command = "make test" })
eq(vim.api.nvim_win_get_config(0).relative ~= "", true, "a float")
local shown = table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), "\n")
eq(shown:find("make test", 1, true) ~= nil, true, "the command is shown")
vim.api.nvim_feedkeys("y", "x", false)
eq(result, { behavior = "allow", updatedInput = { command = "make test" } }, "allowed")

-- n denies
ask("WebFetch", { url = "https://example.com" })
vim.api.nvim_feedkeys("n", "x", false)
eq(result.behavior, "deny", "denied")

-- a allows this time and the rest of the session, for that command only
ask("Bash", { command = "go test ./..." })
vim.api.nvim_feedkeys("a", "x", false)
eq(result.behavior, "allow", "allowed for the session")
ask("Bash", { command = "go test ./..." })
eq(result and result.behavior, "allow", "the same command again isn't asked")
ask("Bash", { command = "rm -rf build" })
eq(result, nil, "a different command still asks")
vim.api.nvim_feedkeys("n", "x", false)
ask("Bash", { command = "go test ./..." }, tmp .. "/other")
eq(result, nil, "another project still asks")
vim.api.nvim_feedkeys("n", "x", false)

-- two at once wait their turn
local answers = {}
for _, cmd in ipairs({ "first", "second" }) do
  perms.request({
    tool = "Bash",
    input = { command = cmd },
    respond = function(r)
      table.insert(answers, cmd .. ":" .. r.behavior)
    end,
  })
end
eq(perms.pending(), 2, "both waiting")
vim.api.nvim_feedkeys("y", "x", false)
wait(function()
  return #answers == 1 and vim.api.nvim_win_get_config(0).relative ~= ""
end, "the second prompt")
vim.api.nvim_feedkeys("n", "x", false)
wait(function()
  return #answers == 2
end, "both answered")
eq(answers, { "first:allow", "second:deny" }, "in order")

-- an edit is a diff; accepting it unchanged allows the edit as asked
local tab = vim.api.nvim_get_current_tabpage()
ask("Edit", { file_path = file, old_string = "one", new_string = "uno" })
eq(vim.api.nvim_get_current_tabpage() ~= tab, true, "a diff tab")
eq(vim.api.nvim_buf_get_lines(0, 0, 1, false), { "uno" }, "the proposal")
vim.api.nvim_feedkeys("ga", "x", false)
eq(
  result,
  { behavior = "allow", updatedInput = { file_path = file, old_string = "one", new_string = "uno" } },
  "edit allowed"
)
eq(vim.api.nvim_get_current_tabpage(), tab, "diff closed")

-- changing the proposal first allows what you ended up with
ask("Edit", { file_path = file, old_string = "one", new_string = "uno" })
vim.api.nvim_buf_set_lines(0, 0, 1, false, { "eins" })
vim.cmd.write()
eq(
  result.updatedInput,
  { file_path = file, old_string = "one\ntwo\ntwo\n", new_string = "eins\ntwo\ntwo\n" },
  "your version"
)

-- q rejects
ask("Write", { file_path = tmp .. "/new.txt", content = "hi\n" })
vim.api.nvim_feedkeys("q", "x", false)
eq(result.behavior, "deny", "write rejected")

-- closing the diff any other way is a no, said at once: the proposal (:q,
-- its buffer wiped), the file's side, <C-w>o in the proposal, the whole tab
local closes = {
  {
    "quit",
    function()
      vim.cmd.quit()
    end,
  },
  {
    "the file's side",
    function()
      vim.cmd.wincmd("h")
      vim.cmd.quit()
    end,
  },
  {
    "only",
    function()
      vim.cmd.only()
    end,
  },
  {
    "tabclose",
    function()
      vim.cmd.tabclose()
    end,
  },
  {
    "bwipeout",
    function()
      vim.cmd("bwipeout!")
    end,
  },
}
for _, c in ipairs(closes) do
  ask("Write", { file_path = tmp .. "/new.txt", content = "hi\n" })
  c[2]()
  eq(result and result.behavior, "deny", "rejected at once by " .. c[1])
  wait(function()
    return vim.api.nvim_get_current_tabpage() == tab
  end, "the diff's tab to go after " .. c[1])
end

-- :wq, :x and ZZ accept and close the diff, and only the diff: the quit doesn't
-- land on one of your windows (here the only one, which would quit Neovim);
-- :x and ZZ accept even with nothing edited, where they'd otherwise just close
local wins = vim.api.nvim_tabpage_list_wins(tab)
local accepts = {
  {
    ":wq",
    function()
      vim.cmd.wq()
    end,
  },
  {
    ":x",
    function()
      vim.api.nvim_feedkeys(":x\r", "tx", false)
    end,
  },
  {
    "ZZ",
    function()
      vim.api.nvim_feedkeys("ZZ", "x", false)
    end,
  },
}
for _, a in ipairs(accepts) do
  ask("Write", { file_path = tmp .. "/new.txt", content = "hi\n" })
  a[2]()
  eq(result and result.behavior, "allow", a[1] .. " accepts")
  wait(function()
    return vim.api.nvim_get_current_tabpage() == tab
  end, "the diff's tab to go after " .. a[1])
  eq(vim.api.nvim_tabpage_list_wins(tab), wins, "your windows untouched by " .. a[1])
end

-- :wa from your own tab doesn't accept an edited proposal; it stays open, unsaved
ask("Write", { file_path = tmp .. "/new.txt", content = "hi\n" })
local review_tab = vim.api.nvim_get_current_tabpage()
vim.api.nvim_buf_set_lines(0, 0, 1, false, { "half done" })
vim.api.nvim_set_current_tabpage(tab)
pcall(vim.cmd.wall)
eq(result, nil, ":wa elsewhere doesn't answer")
vim.api.nvim_set_current_tabpage(review_tab)
eq(vim.bo.modified, true, "the proposal still unsaved")
vim.api.nvim_feedkeys("q", "x", false)
eq(result.behavior, "deny", "then q rejects it")

-- closing a review while another waits: the next one opens and keeps the
-- cursor, wherever 'tabclose' would send it
vim.o.tabclose = "left"
local first, second
perms.request({
  tool = "Write",
  input = { file_path = tmp .. "/one.txt", content = "1\n" },
  root = tmp,
  respond = function(r)
    first = r
  end,
})
perms.request({
  tool = "Write",
  input = { file_path = tmp .. "/two.txt", content = "2\n" },
  root = tmp,
  respond = function(r)
    second = r
  end,
})
vim.cmd.quit()
eq(first and first.behavior, "deny", "the first rejected")
wait(function()
  return vim.api.nvim_buf_get_name(0):find("two.txt", 1, true) ~= nil
end, "the second review to have the cursor")
vim.api.nvim_feedkeys("q", "x", false)
eq(second and second.behavior, "deny", "the second answered too")
vim.o.tabclose = ""

vim.fn.delete(tmp, "rf")
print("permissions_test: ok")
