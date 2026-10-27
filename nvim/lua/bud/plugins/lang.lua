-- Languages (D29): each plugin wires up its part of the enabled language specs,
-- which bud.lang reads from languages/. Parsers and Mason packages are installed
-- by the restore hooks, while bud install / bud update runs, never at startup.
--
-- Highlighting, servers and linting start on vim.schedule, so a file shows
-- before they're ready (D35): each can cost 40-120 ms the first time (compiling
-- TypeScript's queries, tsc's version probe, golangci-lint's), which would
-- otherwise all land before the first screen.

-- ten minutes for a restore hook's downloads and builds
local restore_timeout = 600000

-- Mason's bin dir goes on PATH now, the way mason.setup() does it, so servers,
-- linters and formatters are found without loading mason first
vim.env.MASON = vim.fn.stdpath("data") .. "/mason"
vim.env.PATH = vim.env.MASON .. "/bin:" .. vim.env.PATH

-- the enabled languages' filetypes, which the LSP and lint sides load on; nil with
-- none, since an empty ft list would never load the plugin
local filetypes = vim.tbl_keys(require("bud.lang").filetypes())
table.sort(filetypes)
if #filetypes == 0 then
  filetypes = nil
end

return {
  {
    src = "mason-org/mason.nvim",
    version = vim.version.range("2"),
    -- ~3 ms to set up, and PATH is handled above, so only its UI loads it
    cmd = { "Mason", "MasonInstall", "MasonLog" },
    keys = { { "<leader>M", desc = "Mason" } },
    config = function()
      -- PATH is already set, so mason must not prepend its bin dir again
      require("mason").setup({ PATH = "skip" })
      vim.keymap.set("n", "<leader>M", "<cmd>Mason<cr>", { desc = "Mason" })
    end,
    restore = function()
      local names = require("bud.lang").collect("mason")
      if #names == 0 then
        return
      end
      require("mason").setup({ PATH = "skip" })
      local registry = require("mason-registry")
      local ok, err = registry.refresh()
      if not ok then
        error("refreshing the Mason registry: " .. tostring(err), 0)
      end

      local pending, failed = 0, {}
      for _, name in ipairs(names) do
        local found, pkg = pcall(registry.get_package, name)
        if not found then
          table.insert(failed, name .. " (no such Mason package)")
        elseif not pkg:is_installed() then
          pending = pending + 1
          vim.api.nvim_echo({ { "bud: installing " .. name .. "\n" } }, true, {})
          pkg:install({}, function(success, result)
            if not success then
              table.insert(failed, ("%s (%s)"):format(name, tostring(result)))
            end
            pending = pending - 1
          end)
        end
      end
      if not vim.wait(restore_timeout, function()
        return pending == 0
      end, 200) then
        error("timed out installing Mason packages", 0)
      end
      if #failed > 0 then
        error("installing Mason packages failed: " .. table.concat(failed, ", "), 0)
      end
    end,
  },
  {
    -- server settings for vim.lsp.config: its lsp/<name>.lua files merge under ours
    src = "neovim/nvim-lspconfig",
    version = vim.version.range("2"),
    -- loading vim.lsp costs ~4 ms, so wait for a buffer that needs it
    ft = filetypes,
    config = function()
      -- a server that isn't installed is skipped (and logged) when a buffer opens
      vim.schedule(function()
        for server, settings in pairs(require("bud.lang").servers()) do
          vim.lsp.config(server, settings)
          vim.lsp.enable(server)
        end
        -- enable() re-runs its FileType handler on open buffers, but only once
        -- VimEnter has fired; before that (a -c command), do it here, so the
        -- buffer that loaded this plugin still gets its servers
        if vim.v.vim_did_enter == 0 then
          for _, buf in ipairs(vim.api.nvim_list_bufs()) do
            if vim.api.nvim_buf_is_loaded(buf) and vim.bo[buf].filetype ~= "" then
              pcall(vim.api.nvim_exec_autocmds, "FileType", { group = "nvim.lsp.enable", buffer = buf })
            end
          end
        end
      end)
    end,
  },
  {
    src = "nvim-treesitter/nvim-treesitter",
    -- main is the rewrite for Neovim 0.12; master is frozen
    version = "main",
    config = function()
      if not filetypes then
        return
      end
      vim.api.nvim_create_autocmd("FileType", {
        group = vim.api.nvim_create_augroup("bud.lang.treesitter", {}),
        pattern = filetypes,
        desc = "start tree-sitter highlighting",
        callback = function(ev)
          vim.schedule(function()
            if vim.api.nvim_buf_is_valid(ev.buf) then
              -- fails when the parser isn't built yet, which leaves regex highlighting on
              pcall(vim.treesitter.start, ev.buf)
            end
          end)
        end,
      })
    end,
    restore = function()
      local ts = require("nvim-treesitter")
      -- parsers have to match the queries of the plugin revision just checked out
      if not ts.update():wait(restore_timeout) then
        error("updating tree-sitter parsers failed (bud doctor checks the tree-sitter CLI)", 0)
      end
      local parsers = require("bud.lang").collect("treesitter")
      if #parsers > 0 and not ts.install(parsers, { summary = true }):wait(restore_timeout) then
        error("building tree-sitter parsers failed (bud doctor checks the tree-sitter CLI)", 0)
      end
    end,
  },
  {
    src = "stevearc/conform.nvim",
    version = vim.version.range("9"),
    keys = { { "<leader>cf", desc = "Format buffer", mode = { "n", "x" } } },
    event = "BufWritePre",
    cmd = "ConformInfo",
    config = function()
      local lang = require("bud.lang")
      local enabled = lang.filetypes()
      require("conform").setup({
        formatters_by_ft = lang.by_ft("format"),
        -- only in projects with a Prettier config, so format on save doesn't
        -- rewrite every file of a repo that doesn't use it
        formatters = { prettier = { require_cwd = true } },
        -- a language with no formatters gets its LSP server's formatting
        default_format_opts = { lsp_format = "fallback" },
        -- only bud's languages, so a stray file isn't reformatted by surprise
        format_on_save = function(buf)
          local ft = vim.bo[buf].filetype
          if not lang.format_on_save() or not enabled[ft] then
            return
          end
          -- a spec can ask for a style file first (C's .clang-format)
          local markers = lang.format_markers(ft)
          local file = vim.api.nvim_buf_get_name(buf)
          if markers and not vim.fs.find(markers, { path = vim.fs.dirname(file), upward = true })[1] then
            return
          end
          return { timeout_ms = 1000 }
        end,
      })
      vim.keymap.set({ "n", "x" }, "<leader>cf", function()
        require("conform").format({ async = true })
      end, { desc = "Format buffer" })
    end,
  },
  {
    src = "mfussenegger/nvim-lint",
    ft = filetypes,
    config = function()
      local lint = require("lint")
      lint.linters_by_ft = require("bud.lang").by_ft("lint")
      -- a linter that isn't installed is skipped rather than warned about on every write
      local function installed(linter)
        local cmd = linter.cmd
        if type(cmd) == "function" then
          cmd = cmd()
        end
        return type(cmd) == "string" and vim.fn.executable(cmd) == 1
      end
      -- FileType rather than BufReadPost, which runs before the filetype is known
      vim.api.nvim_create_autocmd({ "FileType", "BufWritePost", "InsertLeave" }, {
        group = vim.api.nvim_create_augroup("bud.lang.lint", {}),
        desc = "lint the buffer",
        callback = function(ev)
          vim.schedule(function()
            if vim.api.nvim_buf_is_valid(ev.buf) then
              vim.api.nvim_buf_call(ev.buf, function()
                lint.try_lint(nil, { filter = installed })
              end)
            end
          end)
        end,
      })
    end,
  },
}
