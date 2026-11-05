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
    -- checkboxes (D88). Normal mode shows the cursor line as written, Insert
    -- mode the whole file, and <leader>um flips a buffer between the two
    src = "MeanderingProgrammer/render-markdown.nvim",
    version = vim.version.range("8"),
    ft = "markdown",
    -- the plugin reads this as it loads, so even the buffer that loads it gets
    -- bud's settings (setup() afterwards would be too late for that one)
    init = function()
      vim.g.render_markdown_config = {
        file_types = { "markdown" },
        -- ui.markdown = false: files open as text, and <leader>um still renders one
        enabled = require("bud.config").get("ui.markdown", true) ~= false,
        -- files only: hover docs, the issue float and other scratch buffers
        -- that set filetype=markdown stay as they are
        ignore = function(buf)
          return vim.bo[buf].buftype ~= ""
        end,
        -- a diff (Claude's edit review, diffview) shows the text on both sides,
        -- so a changed # or list marker is never hidden
        render = { diff = false },
        on = {
          -- only buffers it attached to, so the key never does nothing
          attach = function(ctx)
            vim.keymap.set("n", "<leader>um", function()
              require("render-markdown").buf_toggle()
            end, { buffer = ctx.buf, desc = "Toggle markdown rendering" })
          end,
        },
      }
    end,
  },
}
