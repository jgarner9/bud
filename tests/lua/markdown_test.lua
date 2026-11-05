-- Run with: nvim --clean --headless -l tests/lua/markdown_test.lua (just lua-test)

vim.opt.rtp:prepend(vim.fn.getcwd() .. "/nvim")

local function eq(got, want, what)
  if not vim.deep_equal(got, want) then
    error(("%s: got %s, want %s"):format(what, vim.inspect(got), vim.inspect(want)), 2)
  end
end

local config = require("bud.config")
config.file = vim.fn.tempname()
local function settings(json)
  vim.fn.writefile({ json }, config.file)
end

local md = require("bud.markdown")
-- the plugin's set_buf, recorded as { buffer it acted on, on or off }
local calls = {}
md.api = function()
  return {
    set_buf = function(on)
      table.insert(calls, { vim.api.nvim_get_current_buf(), on })
    end,
  }
end

local function file_buf(lines)
  local path = vim.fn.tempname() .. ".md"
  vim.fn.writefile(lines or { "# title", "", "- item" }, path)
  vim.cmd.edit(vim.fn.fnameescape(path))
  return vim.api.nvim_get_current_buf()
end

-- the config the plugin reads skips anything that isn't a file
local cfg = md.config()
local scratch = vim.api.nvim_create_buf(false, true)
eq(cfg.ignore(scratch), true, "a scratch buffer is ignored")
local buf = file_buf()
eq(cfg.ignore(buf), false, "a file isn't ignored")
eq(cfg.render.diff, false, "diff windows don't render")

-- attaching with the default leaves it rendered and maps the toggle there
settings("{}")
cfg.on.attach({ buf = buf })
eq(calls, {}, "nothing to change by default")
eq(vim.fn.maparg("<leader>um", "n", false, true).buffer, 1, "the toggle is buffer-local")

md.toggle(buf)
eq(calls, { { buf, false } }, "the toggle shows the text")
md.toggle(buf)
eq(calls, { { buf, false }, { buf, true } }, "and renders it again")

-- ui.markdown = false starts a new file as text
calls = {}
settings('{"ui": {"markdown": false}}')
local off = file_buf()
md.attach({ buf = off })
eq(calls, { { off, false } }, "ui.markdown = false starts as text")
md.toggle(off)
eq(calls[2], { off, true }, "and the toggle still renders one")

-- a window in diff mode shows the text, even with the buffer open elsewhere
calls = {}
vim.api.nvim_set_current_buf(buf)
vim.cmd("vsplit")
local diff_win = vim.api.nvim_get_current_win()
vim.wo[diff_win].diff = true
md.sync_all()
eq(calls, { { buf, false } }, "a buffer in a diff shows its text")
md.sync_all()
eq(#calls, 1, "and isn't set again")
vim.api.nvim_win_close(diff_win, true)
md.sync_all()
eq(calls[2], { buf, true }, "renders again once the diff is gone")

-- a buffer switched to text stays text after a diff
calls = {}
md.toggle(buf)
vim.cmd("vsplit")
vim.wo.diff = true
md.sync_all()
vim.cmd("close")
md.sync_all()
eq(calls, { { buf, false } }, "a buffer you switched to text stays text")

-- a buffer the plugin never attached to is left alone
md.toggle(scratch)
md.sync(scratch)
eq(#calls, 1, "an unattached buffer is left alone")

-- the toggle does nothing while the file is in a diff, and says why
calls = {}
md.toggle(buf)
eq(calls, { { buf, true } }, "toggled back on for the next checks")
vim.cmd("vsplit")
vim.wo.diff = true
md.sync_all()
local said
local notify = vim.notify
vim.notify = function(msg)
  said = msg
end
md.toggle(buf)
vim.notify = notify
eq(vim.b[buf].bud_markdown, true, "the choice is kept during a diff")
eq(said ~= nil, true, "and the toggle says why nothing changed")
vim.cmd("close")
md.sync_all()

-- with setup, windows going in and out of diff mode are followed by themselves
md.setup()
calls = {}
vim.cmd("vsplit")
vim.cmd.diffthis()
eq(calls, { { buf, false } }, "diffthis shows the text")
vim.cmd("close")
if not vim.wait(1000, function()
  return #calls == 2
end, 10) then
  error("closing the diff window didn't render it again: " .. vim.inspect(calls))
end
eq(calls[2], { buf, true }, "closing the diff window renders it again")

-- a reopened file (:bdelete, then :edit) is set up again, as ui.markdown says now
settings("{}")
local path = vim.api.nvim_buf_get_name(buf)
vim.cmd("enew")
vim.cmd("bdelete " .. buf)
vim.cmd.edit(vim.fn.fnameescape(path))
eq(vim.api.nvim_get_current_buf(), buf, "the same buffer comes back")
eq(vim.b[buf].bud_markdown, true, "its choice is set up again")
eq(vim.fn.maparg("<leader>um", "n", false, true).buffer, 1, "and its toggle")

-- open hands the plugin's FileType handler each markdown file already open
local handed = {}
vim.api.nvim_create_autocmd("FileType", {
  group = vim.api.nvim_create_augroup("RenderMarkdown", {}),
  callback = function(ev)
    table.insert(handed, ev.buf)
  end,
})
local md_scratch = vim.api.nvim_create_buf(false, true)
vim.bo[md_scratch].filetype = "markdown"
handed = {}
md.open()
table.sort(handed)
local want = { buf, off }
table.sort(want)
eq(handed, want, "open hands over the markdown files, not scratch buffers")

print("markdown_test: ok")
