-- Run with: nvim --clean --headless -l tests/lua/explorer_test.lua (just lua-test)

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")
dofile("tests/lua/lib/plugin.lua")("oil.nvim", "lua/oil/init.lua")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local function wait(what, cond)
  if not vim.wait(2000, cond, 10) then
    error("timed out: " .. what, 2)
  end
end

local tmp = vim.uv.fs_realpath(vim.fn.tempname()) or vim.fn.tempname()
vim.fn.mkdir(tmp .. "/sub", "p")
tmp = vim.uv.fs_realpath(tmp)
vim.fn.writefile({ "hello" }, tmp .. "/a.txt")
vim.cmd.cd(vim.fn.fnameescape(tmp))

require("oil").setup({
  default_file_explorer = true,
  keymaps = {
    ["<CR>"] = {
      callback = function()
        require("bud.explorer").select()
      end,
      mode = "n",
    },
  },
})
local explorer = require("bud.explorer")
explorer.setup()
local cursor = require("bud.ui.cursor")
local guicursor = vim.o.guicursor
local hidden = guicursor .. ",n:block-" .. cursor.group

-- the cursor helper: hidden while anyone wants it, back once nobody does
cursor.hide("a")
cursor.hide("b")
eq(vim.o.guicursor, hidden, "hidden")
cursor.show("a")
eq(vim.o.guicursor, hidden, "still hidden for b")
cursor.show("b")
eq(vim.o.guicursor, guicursor, "back")

local function oil_rows()
  return vim.api.nvim_buf_get_lines(vim.api.nvim_win_get_buf(explorer.win()), 0, -1, false)
end

-- <leader>e opens the pane, narrow, focused, cursor hidden
local editor = vim.api.nvim_get_current_win()
explorer.toggle()
local pane = explorer.win()
eq(pane ~= nil, true, "open")
eq(vim.api.nvim_get_current_win(), pane, "focused")
eq(vim.api.nvim_win_get_width(pane), explorer.width, "narrow")
eq(vim.o.guicursor, hidden, "cursor hidden in the pane")
wait("oil lists the cwd", function()
  return vim.bo[vim.api.nvim_win_get_buf(pane)].filetype == "oil" and #table.concat(oil_rows()) > 0
end)

-- again goes back to where it came from, cursor and all
explorer.toggle()
eq(vim.api.nvim_get_current_win(), editor, "back to the editor")
eq(vim.o.guicursor, guicursor, "cursor back")

-- from another window, <leader>e goes to the pane and back to that one
vim.cmd("vsplit")
local other = vim.api.nvim_get_current_win()
explorer.toggle()
eq(vim.api.nvim_get_current_win(), pane, "to the pane")
explorer.toggle()
eq(vim.api.nvim_get_current_win(), other, "back to the window it came from")
vim.api.nvim_win_close(other, true)

-- <CR> on a file opens it in the editor window, not the pane
explorer.toggle()
local row
wait("a.txt listed", function()
  for i, l in ipairs(oil_rows()) do
    if l:find("a.txt", 1, true) then
      row = i
      return true
    end
  end
end)
vim.api.nvim_win_set_cursor(pane, { row, 0 })
explorer.select()
eq(vim.api.nvim_get_current_win(), editor, "opened in the editor")
eq(vim.api.nvim_buf_get_name(0), tmp .. "/a.txt", "the file")
eq(vim.bo[vim.api.nvim_win_get_buf(pane)].filetype, "oil", "the pane still shows oil")

-- <leader>E closes it; the cursor stays visible
explorer.toggle()
explorer.close()
eq(explorer.win(), nil, "closed")
eq(vim.api.nvim_win_is_valid(pane), false, "window gone")
eq(vim.api.nvim_get_current_win(), editor, "back in the editor")
eq(vim.o.guicursor, guicursor, "cursor back after closing")

-- the layout opens it without the focus
explorer.open({ side = "left", size = 20 })
eq(vim.api.nvim_get_current_win(), editor, "layout open keeps the editor")
eq(vim.api.nvim_win_get_width(explorer.win()), 20, "the layout's size")
eq(vim.o.guicursor, guicursor, "cursor visible outside the pane")

print("explorer_test: ok")
