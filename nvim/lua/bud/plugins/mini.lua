-- mini.nvim is one plugin holding many small modules; bud sets up the ones it
-- uses here (D29), so other plugin files never add mini.nvim a second time.

local map = vim.keymap.set

return {
  {
    src = "echasnovski/mini.nvim",
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

      require("mini.statusline").setup({
        content = { active = require("bud.statusline").active },
      })
    end,
  },
}
