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
  { src = "MunifTanjim/nui.nvim" },
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
    src = "folke/which-key.nvim",
    config = function()
      require("which-key").setup({})
    end,
  },
}
