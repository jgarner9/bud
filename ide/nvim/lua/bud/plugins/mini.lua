-- The mini.nvim modules bud uses (D29), each from its own standalone repo: the
-- whole of mini.nvim is ~26 MB, most of it tests, for a few modules (D68).

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
  {
    -- notifications in a stack at the top right, language servers' progress
    -- among them (JGA-138); everything bud reports through vim.notify lands here
    src = "nvim-mini/mini.notify",
    version = vim.version.range("0"),
    -- just after the first screen: ~1 ms the budget doesn't have (D35);
    -- anything earlier shows the plain way
    event = "UIEnter",
    config = function()
      local notify = require("mini.notify")
      notify.setup({
        content = {
          -- no timestamp: the message is enough
          format = function(n)
            return n.msg
          end,
        },
        window = { config = { border = "rounded" }, max_width_share = 0.4, winblend = 0 },
      })
      vim.notify = notify.make_notify({
        ERROR = { duration = 8000 },
        WARN = { duration = 5000 },
        INFO = { duration = 3000 },
      })
      vim.keymap.set("n", "<leader>un", notify.show_history, { desc = "Notification history" })
    end,
  },
}
