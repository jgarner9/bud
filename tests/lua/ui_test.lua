-- Run with: nvim --clean --headless -l tests/lua/ui_test.lua (just lua-test)

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")
dofile("tests/lua/lib/nui.lua")

local ui = require("bud.ui")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

-- keys go through the typeahead, like a person pressing them
local function press(keys)
  vim.api.nvim_feedkeys(vim.api.nvim_replace_termcodes(keys, true, false, true), "xt", false)
  vim.wait(50)
end

local function floats()
  local n = 0
  for _, w in ipairs(vim.api.nvim_list_wins()) do
    if vim.api.nvim_win_get_config(w).relative ~= "" then
      n = n + 1
    end
  end
  return n
end

-- a float shows its lines, title and footer; q closes it and on_close runs once
local closed = 0
local h = ui.float({
  title = "hello",
  footer = "q close",
  lines = { "one", "two" },
  on_close = function()
    closed = closed + 1
  end,
})
eq(vim.api.nvim_get_current_win(), h.win, "entered")
eq(vim.api.nvim_buf_get_lines(h.buf, 0, -1, false), { "one", "two" }, "lines")
eq(vim.bo[h.buf].modifiable, false, "read-only")
-- nui draws the border, title and footer in a window of their own
local frame = vim.api.nvim_buf_get_lines(vim.api.nvim_win_get_buf(h.popup.border.winid), 0, -1, false)
assert(frame[1]:find("hello", 1, true) and frame[#frame]:find("q close", 1, true), vim.inspect(frame))
press("q")
vim.wait(50)
eq({ floats(), closed }, { 0, 1 }, "closed by q")

-- a dialog runs the action whose key was pressed, once
local got = {}
ui.dialog({
  title = "permission",
  lines = { "Claude wants to run ls" },
  actions = {
    {
      key = "y",
      label = "allow",
      fn = function()
        table.insert(got, "y")
      end,
    },
    {
      key = { "n", "q", "<Esc>" },
      label = "deny",
      fn = function()
        table.insert(got, "n")
      end,
    },
  },
  cancel = function()
    table.insert(got, "cancel")
  end,
})
eq(ui.actions_footer({ { key = "y", label = "allow" }, { key = "n", label = "deny" } }), "y allow   n deny", "footer")
press("y")
vim.wait(50)
eq({ got, floats() }, { { "y" }, 0 }, "dialog y")

-- closed some other way: cancel answers for it
got = {}
h = ui.dialog({
  title = "t",
  lines = { "x" },
  actions = { { key = "y", label = "yes" } },
  cancel = function()
    table.insert(got, "cancel")
  end,
})
vim.api.nvim_win_close(h.win, true)
vim.wait(100, function()
  return #got > 0
end)
eq(got, { "cancel" }, "dialog closed")

-- vim.ui.select through the menu: <CR> on the second, and a number key
ui.setup()
local choice, idx
vim.ui.select({ "a", "b", "c" }, { prompt = "Pick:" }, function(c, i)
  choice, idx = c, i
end)
press("j<CR>")
vim.wait(100, function()
  return choice ~= nil
end)
eq({ choice, idx, floats() }, { "b", 2, 0 }, "select with j <CR>")

choice = nil
vim.ui.select({ { n = "x" }, { n = "y" } }, {
  format_item = function(i)
    return i.n
  end,
}, function(c)
  choice = c
end)
eq(vim.api.nvim_buf_get_lines(0, 0, -1, false), { "1  x", "2  y" }, "formatted items")
press("2")
vim.wait(100, function()
  return choice ~= nil
end)
eq(choice, { n = "y" }, "select by number")

-- closing the menu is a nil choice
local answered = false
vim.ui.select({ "a" }, {}, function(c)
  answered = true
  choice = c
end)
press("q")
vim.wait(200, function()
  return answered
end)
eq({ answered, choice }, { true, nil }, "select closed")

-- input: what's submitted, and nil when closed. A headless run can't type
-- into a prompt buffer, so this submits the way <CR> does
local text
answered = false
h = ui.input({ prompt = "Name: ", default = "bu" }, function(t)
  answered = true
  text = t
end)
eq(vim.api.nvim_buf_get_lines(h.buf, 0, -1, false), { " bu" }, "default")
-- let nui finish opening it, as it would before anyone typed
vim.wait(20)
h.popup.input_props.on_submit("bud")
vim.wait(200, function()
  return answered
end)
eq({ text, floats() }, { "bud", 0 }, "input")
answered = false
h = ui.input({ prompt = "Name: " }, function(t)
  answered = true
  text = t
end)
vim.wait(20)
press("<Esc>")
vim.wait(200, function()
  return answered
end)
eq({ answered, text, floats() }, { true, nil, 0 }, "input cancelled")

-- a panel: boxes by name, <Tab> between them, q closes all
local panel_closed = false
local p = ui.panel({
  boxes = {
    { name = "transcript", size = "70%", title = "chat", lines = { "hi" }, modifiable = false },
    { name = "prompt", size = "30%", title = "you", enter = true },
  },
  on_close = function()
    panel_closed = true
  end,
})
eq(vim.api.nvim_get_current_win(), p.boxes.prompt.win, "prompt entered")
eq(vim.api.nvim_buf_get_lines(p.boxes.transcript.buf, 0, -1, false), { "hi" }, "transcript lines")
press("<Tab>")
eq(vim.api.nvim_get_current_win(), p.boxes.transcript.win, "tab moves")
press("q")
vim.wait(100, function()
  return panel_closed
end)
eq({ panel_closed, floats() }, { true, 0 }, "panel closed")

print("ui_test: ok")
