-- Looks (D29). The statusline is mini.statusline, set up in mini.lua.

return {
  {
    src = "catppuccin/nvim",
    name = "catppuccin",
    version = vim.version.range("2"),
    config = function()
      -- integrations for installed plugins (blink, gitsigns, mini, neo-tree, ...) turn on by themselves
      require("catppuccin").setup({ flavour = "macchiato" })
      vim.cmd.colorscheme("catppuccin")
    end,
  },
  {
    -- breadcrumbs at the top of file windows, each part a menu (JGA-139)
    src = "Bekaboo/dropbar.nvim",
    version = vim.version.range("14"),
    -- just after the first screen (D35); setup attaches to the windows open by then
    event = "UIEnter",
    config = function()
      if require("bud.config").get("ui.breadcrumbs", true) == false then
        vim.g.loaded_dropbar = true -- keeps its own FileType setup from running
        return
      end
      require("dropbar").setup({
        bar = {
          -- files only: not the tree, terminals, help, the dashboard or chat
          enable = function(buf, win)
            if
              not vim.api.nvim_buf_is_valid(buf)
              or not vim.api.nvim_win_is_valid(win)
              or vim.fn.win_gettype(win) ~= ""
              or vim.bo[buf].buftype ~= ""
              or vim.api.nvim_buf_get_name(buf) == ""
              or vim.wo[win].winbar ~= ""
            then
              return false
            end
            local stat = vim.uv.fs_stat(vim.api.nvim_buf_get_name(buf))
            return not (stat and stat.size > 1024 * 1024)
          end,
        },
      })
      local api = require("dropbar.api")
      vim.keymap.set("n", "<leader>;", api.pick, { desc = "Pick a breadcrumb" })
      vim.keymap.set("n", "[;", api.goto_context_start, { desc = "Start of the current context" })
      vim.keymap.set("n", "];", api.select_next_context, { desc = "Next context" })
    end,
  },
  {
    -- markdown files render in place: headings, lists, code blocks, tables,
    -- checkboxes (D88). Insert mode shows the cursor line as written, and
    -- <leader>um flips a buffer between rendered and plain text to edit
    src = "MeanderingProgrammer/render-markdown.nvim",
    version = vim.version.range("8"),
    ft = "markdown",
    config = function()
      local md = require("render-markdown")
      -- files only: hover docs, the issue float and other scratch buffers that
      -- set filetype=markdown stay as they are
      local function file(buf)
        return vim.bo[buf].buftype == ""
      end
      md.setup({
        file_types = { "markdown" },
        ignore = function(buf)
          return not file(buf)
        end,
      })
      -- the plugin attaches to the buffer that loaded it before setup() runs,
      -- so ignore hasn't seen that one
      if not file(0) then
        md.buf_disable()
      end
      vim.api.nvim_create_autocmd("FileType", {
        group = vim.api.nvim_create_augroup("bud.markdown", {}),
        pattern = "markdown",
        desc = "<leader>um toggles markdown rendering",
        callback = function(ev)
          if file(ev.buf) then
            vim.keymap.set("n", "<leader>um", md.buf_toggle, { buffer = ev.buf, desc = "Toggle markdown rendering" })
          end
        end,
      })
    end,
  },
}
