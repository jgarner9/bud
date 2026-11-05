-- Markdown rendering (D88): render-markdown.nvim draws markdown files in
-- place, and this decides, per buffer, whether it should. A buffer renders
-- unless you switched it to text (<leader>um; ui.markdown = false starts new
-- ones that way) or one of its windows is a diff. The plugin draws per buffer,
-- not per window, so a file open both in the editor and in Claude's edit
-- review (or diffview) would otherwise hide a changed # or list marker there.

local M = {}

-- the plugin's API; tests swap it
M.api = function()
  return require("render-markdown")
end

-- file is whether buf is a file, the only buffers that render: hover docs,
-- the issue float and other scratch buffers that set filetype=markdown don't
function M.file(buf)
  return vim.bo[buf].buftype == ""
end

local function in_diff(buf)
  for _, win in ipairs(vim.fn.win_findbuf(buf)) do
    if vim.wo[win].diff then
      return true
    end
  end
  return false
end

-- sync renders buf or not, from what you chose and whether it's in a diff
function M.sync(buf)
  if not vim.api.nvim_buf_is_valid(buf) or vim.b[buf].bud_markdown == nil then
    return
  end
  local on = vim.b[buf].bud_markdown and not in_diff(buf)
  if vim.b[buf].bud_markdown_shown == on then
    return
  end
  vim.b[buf].bud_markdown_shown = on
  -- set_buf acts on the current buffer
  vim.api.nvim_buf_call(buf, function()
    M.api().set_buf(on)
  end)
end

-- sync_all syncs every buffer the plugin attached to
function M.sync_all()
  for _, buf in ipairs(vim.api.nvim_list_bufs()) do
    M.sync(buf)
  end
end

-- toggle switches buf (default: the current one) between rendered and text
function M.toggle(buf)
  buf = buf or vim.api.nvim_get_current_buf()
  if vim.b[buf].bud_markdown == nil then
    return
  end
  vim.b[buf].bud_markdown = not vim.b[buf].bud_markdown
  M.sync(buf)
end

-- attach is the plugin's on.attach: the buffer starts as ui.markdown says,
-- read now rather than at startup, so a change applies to the next file
function M.attach(ctx)
  local buf = ctx.buf
  vim.b[buf].bud_markdown = require("bud.config").get("ui.markdown", true) ~= false
  -- what the plugin does by itself once attached
  vim.b[buf].bud_markdown_shown = true
  vim.keymap.set("n", "<leader>um", function()
    M.toggle(buf)
  end, { buffer = buf, desc = "Toggle markdown rendering" })
  M.sync(buf)
end

-- config is what the plugin reads as it loads (vim.g.render_markdown_config),
-- so even the buffer that loads it is set up by bud
function M.config()
  return {
    -- the plugin's defaults too, kept here so a change to them can't slip by
    file_types = { "markdown" },
    render = { diff = false },
    ignore = function(buf)
      return not M.file(buf)
    end,
    on = {
      attach = function(ctx)
        M.attach(ctx)
      end,
    },
  }
end

-- setup follows windows going in and out of diff mode, once the plugin loaded
function M.setup()
  local group = vim.api.nvim_create_augroup("bud.markdown", {})
  vim.api.nvim_create_autocmd("OptionSet", {
    group = group,
    pattern = "diff",
    desc = "markdown shows as text in a diff",
    callback = M.sync_all,
  })
  vim.api.nvim_create_autocmd({ "BufWinEnter", "WinClosed", "DiffUpdated" }, {
    group = group,
    desc = "markdown shows as text in a diff",
    callback = function()
      -- a closed window is still listed until the event is over
      vim.schedule(M.sync_all)
    end,
  })
end

return M
