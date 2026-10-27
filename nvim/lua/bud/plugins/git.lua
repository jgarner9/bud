-- Git (D29): changed lines and hunks in the buffer, and the diff view the
-- review layout uses.

local map = vim.keymap.set

return {
  {
    src = "lewis6991/gitsigns.nvim",
    version = vim.version.range("2"),
    config = function()
      require("gitsigns").setup({
        on_attach = function(buf)
          local gs = require("gitsigns")
          local function bmap(mode, lhs, rhs, desc)
            map(mode, lhs, rhs, { buffer = buf, desc = desc })
          end
          bmap("n", "]h", function()
            gs.nav_hunk("next")
          end, "Next hunk")
          bmap("n", "[h", function()
            gs.nav_hunk("prev")
          end, "Previous hunk")
          bmap("n", "<leader>hs", gs.stage_hunk, "Stage hunk")
          bmap("n", "<leader>hr", gs.reset_hunk, "Reset hunk")
          bmap("n", "<leader>hp", gs.preview_hunk_inline, "Preview hunk")
          bmap("n", "<leader>hb", gs.blame_line, "Blame line")
        end,
      })
    end,
  },
  {
    -- maintained fork of diffview.nvim; same module and commands. Loads on its
    -- first command (~3 ms otherwise). No setup() call: the defaults suit bud.
    -- Its session-restore hook waits too, which is fine until bud has sessions
    src = "dlyongemallo/diffview-plus.nvim",
    cmd = {
      "DiffviewOpen",
      "DiffviewClose",
      "DiffviewToggle",
      "DiffviewFileHistory",
      "DiffviewFocusFiles",
      "DiffviewToggleFiles",
      "DiffviewRefresh",
      "DiffviewLog",
      "DiffviewDiffFiles",
      "DiffviewDiffDirs",
      "DiffviewMergeFiles",
      "DiffviewWorktreeOverview",
    },
    init = function()
      map("n", "<leader>gd", "<cmd>DiffviewOpen<cr>", { desc = "Diff working tree" })
      map("n", "<leader>gh", "<cmd>DiffviewFileHistory %<cr>", { desc = "File history" })
      map("n", "<leader>gH", "<cmd>DiffviewFileHistory<cr>", { desc = "Repo history" })
    end,
  },
}
