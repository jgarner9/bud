-- The two mini.nvim modules bud uses (D29), each from its own standalone repo:
-- the whole of mini.nvim is ~26 MB, most of it tests, for two modules (D68).

local map = vim.keymap.set

return {
  {
    src = "nvim-mini/mini.jump2d",
    version = vim.version.range("0"),
    config = function()
      local jump2d = require("mini.jump2d")
      jump2d.setup({
        -- show two characters of each label up front, like Helix's goto-word
        view = { n_steps_ahead = 1 },
        -- bud maps its own keys below
        mappings = { start_jumping = "" },
      })

      local modes = { "n", "x", "o" }
      map(modes, "gw", function()
        jump2d.start(jump2d.builtin_opts.word_start)
      end, { desc = "Jump to word" })
      map(modes, "s", function()
        jump2d.start(jump2d.builtin_opts.single_character)
      end, { desc = "Jump to character" })
    end,
  },
  {
    src = "nvim-mini/mini.statusline",
    version = vim.version.range("0"),
    config = function()
      require("bud.statusline").setup()
    end,
  },
}
