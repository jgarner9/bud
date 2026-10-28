-- Navigation (BASE.md 3, D29). Keymaps live in each plugin's config, so a
-- plugin that fails to set up takes its keys with it instead of leaving them broken.

local map = vim.keymap.set
local pack = require("bud.pack")

return {
  {
    src = "ibhagwan/fzf-lua",
    config = function()
      -- loading fzf-lua starts a server socket, ~10 ms, so it waits for the first picker key
      local fzf = pack.on_first_use("fzf-lua", function(m)
        m.setup({})
      end)
      local function picker(name)
        return function()
          fzf()[name]()
        end
      end
      -- the two-key open (D39): the same pickers as p and f on the dashboard
      map("n", "<leader>p", function()
        require("bud.open").project()
      end, { desc = "Open project" })
      map("n", "<leader>f", function()
        require("bud.open").file()
      end, { desc = "Find file" })
      map("n", "<leader>/", picker("live_grep"), { desc = "Grep project" })
      map("n", "<leader>b", picker("buffers"), { desc = "Buffers" })
      map("n", "<leader>r", picker("oldfiles"), { desc = "Recent files" })
      map("n", "<leader>x", picker("diagnostics_workspace"), { desc = "Diagnostics" })
      map("n", "<leader>S", picker("lsp_live_workspace_symbols"), { desc = "Workspace symbols" })
    end,
  },

  {
    src = "stevearc/aerial.nvim",
    version = vim.version.range("4"),
    config = function()
      require("aerial").setup({})
      map("n", "<leader>o", "<cmd>AerialToggle<cr>", { desc = "Symbols outline" })
    end,
  },

  -- neo-tree is the side pane only; oil owns directory buffers (`nvim somedir`, `:e .`)
  { src = "nvim-lua/plenary.nvim" },
  {
    -- neo-tree's, and bud's own dialogs, menus and panels (bud.ui, JGA-136):
    -- vim.ui.select and vim.ui.input go through them from the start
    src = "MunifTanjim/nui.nvim",
    config = function()
      require("bud.ui.hook")
    end,
  },
  { src = "nvim-tree/nvim-web-devicons" },
  {
    src = "nvim-neo-tree/neo-tree.nvim",
    version = vim.version.range("3"),
    config = function()
      require("neo-tree").setup({
        filesystem = {
          hijack_netrw_behavior = "disabled",
          -- picks up renames and moves made in oil
          use_libuv_file_watcher = true,
        },
      })
      map("n", "<leader>e", "<cmd>Neotree toggle reveal<cr>", { desc = "File tree" })
    end,
  },
  {
    src = "stevearc/oil.nvim",
    version = vim.version.range("2"),
    config = function()
      require("oil").setup({ default_file_explorer = true })
      map("n", "-", "<cmd>Oil<cr>", { desc = "Parent directory" })
    end,
  },

  {
    -- key hints (JGA-140): press a prefix and wait, and the keys that follow
    -- show up. Replaced which-key, which stopped getting commits
    src = "nvim-mini/mini.clue",
    version = vim.version.range("0"),
    -- just after the first screen (D35); it adds its triggers to open buffers
    event = "UIEnter",
    config = function()
      local clue = require("mini.clue")
      local triggers = {}
      for _, t in ipairs({
        { { "n", "x" }, "<Leader>" },
        { { "n", "x" }, "g" },
        { { "n", "x" }, "z" },
        { { "n", "x" }, "'" },
        { { "n", "x" }, "`" },
        { { "n", "x" }, '"' },
        { { "i", "c" }, "<C-r>" },
        { "n", "<C-w>" },
        { "n", "[" },
        { "n", "]" },
      }) do
        for _, mode in ipairs(type(t[1]) == "table" and t[1] or { t[1] }) do
          table.insert(triggers, { mode = mode, keys = t[2] })
        end
      end
      clue.setup({
        triggers = triggers,
        clues = {
          { mode = "n", keys = "<Leader>a", desc = "+Claude" },
          { mode = "x", keys = "<Leader>a", desc = "+Claude" },
          { mode = "n", keys = "<Leader>c", desc = "+code" },
          { mode = "n", keys = "<Leader>g", desc = "+git" },
          { mode = "n", keys = "<Leader>h", desc = "+hunks" },
          { mode = "n", keys = "<Leader>u", desc = "+interface" },
          clue.gen_clues.builtin_completion(),
          clue.gen_clues.g(),
          clue.gen_clues.marks(),
          clue.gen_clues.registers(),
          -- <C-w> then +, -, <, > keeps resizing until another key
          clue.gen_clues.windows({ submode_resize = true }),
          clue.gen_clues.z(),
        },
        window = { delay = 300, config = { width = "auto", border = "rounded" } },
      })
    end,
  },
}
