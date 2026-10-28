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
}
