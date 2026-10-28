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
}
