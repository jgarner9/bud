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
        local icons = require("bud.icons")
        local symbols_prompt = icons.kinds.Function .. "  "
        -- a glyph in each picker's prompt says what it is; the file lists show
        -- the git status of each file too (file icons are on by default)
        m.setup({
          files = { prompt = icons.file .. "  ", git_icons = true },
          buffers = { prompt = icons.chat .. "  " },
          oldfiles = { prompt = icons.recent .. "  " },
          grep = { prompt = icons.command .. "  " },
          diagnostics = { prompt = icons.warn .. "  " },
          -- workspace and document symbols are copies of symbols made when
          -- fzf-lua loads, so each needs its own
          lsp = {
            symbols = { prompt = symbols_prompt },
            document_symbols = { prompt = symbols_prompt },
            workspace_symbols = { prompt = symbols_prompt },
          },
        })
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

  -- oil owns directory buffers (`nvim somedir`, `:e .`) and is the explorer pane
  -- (bud.explorer, JGA-162); neo-tree is the git pane only
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
    end,
  },
  {
    src = "stevearc/oil.nvim",
    version = vim.version.range("2"),
    config = function()
      require("oil").setup({
        default_file_explorer = true,
        -- in the explorer pane a file opens beside it, not in the pane
        keymaps = {
          ["<CR>"] = {
            callback = function()
              require("bud.explorer").select()
            end,
            desc = "Open",
            mode = "n",
          },
        },
      })
      map("n", "-", "<cmd>Oil<cr>", { desc = "Parent directory" })
      require("bud.explorer").setup()
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
          { mode = "n", keys = "<Leader>a", desc = "+AI" },
          { mode = "x", keys = "<Leader>a", desc = "+AI" },
          { mode = "n", keys = "<Leader>c", desc = "+code" },
          { mode = "n", keys = "<Leader>g", desc = "+git" },
          { mode = "n", keys = "<Leader>h", desc = "+hunks" },
          { mode = "n", keys = "<Leader>q", desc = "+quit" },
          { mode = "n", keys = "<Leader>u", desc = "+interface" },
          clue.gen_clues.builtin_completion(),
          clue.gen_clues.g(),
          clue.gen_clues.marks(),
          clue.gen_clues.registers(),
          -- <C-w> then +, -, <, > keeps resizing until another key
          clue.gen_clues.windows({ submode_resize = true }),
          clue.gen_clues.z(),
        },
        -- a narrow panel on the right, as which-key's was: as wide as its keys
        -- and descriptions, but at most 40% of the editor (30 to 60 columns),
        -- so a long list like g's never stretches across the bottom
        window = {
          delay = 300,
          config = function(buf)
            local need = 0
            for _, line in ipairs(vim.api.nvim_buf_get_lines(buf, 0, -1, false)) do
              need = math.max(need, vim.fn.strdisplaywidth(line) + 1)
            end
            local cap = math.min(math.max(math.floor(vim.o.columns * 0.4), 30), 60)
            return { width = math.min(need, cap), border = "rounded" }
          end,
        },
      })
    end,
  },
}
