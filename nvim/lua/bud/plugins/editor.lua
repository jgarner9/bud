-- Editing: completion (D29).

return {
  {
    -- v2 is a rewrite still in progress; v1 is the stable line
    src = "Saghen/blink.cmp",
    version = vim.version.range("1"),
    config = function()
      require("blink.cmp").setup({
        -- <C-space> opens, <C-y> accepts, <C-n>/<C-p> move: Neovim's own completion keys
        keymap = { preset = "default" },
        sources = { default = { "lsp", "path", "snippets", "buffer" } },
        fuzzy = { implementation = "prefer_rust_with_warning" },
      })
    end,
    -- fetch the prebuilt matcher for the locked release tag while bud install or
    -- bud update runs, so the editor never waits on (or needs) the network for it
    restore = function()
      local done, failed
      require("blink.cmp.fuzzy.download").ensure_downloaded(function(err)
        failed = err
        done = true
      end)
      if not vim.wait(120000, function()
        return done
      end, 100) then
        error("timed out downloading blink.cmp's fuzzy matcher")
      end
      if failed then
        error("downloading blink.cmp's fuzzy matcher: " .. tostring(failed))
      end
    end,
  },
}
