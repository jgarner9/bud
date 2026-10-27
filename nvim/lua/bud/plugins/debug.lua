-- Debugging (JGA-108): nvim-dap drives the debug adapters each language spec
-- names (its dap field, bud.dap.adapters), and nvim-dap-view is the UI, one
-- panel that opens with a session and closes after it. Both wait for the
-- first debug key, so they cost nothing at startup. <leader>d is the
-- dashboard, so debugging is on the F-keys plus <leader>B and <leader>D.

local keys = {
  { "<F5>", desc = "Debug: start or continue" },
  { "<F9>", desc = "Debug: toggle breakpoint" },
  { "<F10>", desc = "Debug: step over" },
  { "<F11>", desc = "Debug: step into" },
  { "<S-F11>", desc = "Debug: step out" },
  { "<leader>B", desc = "Toggle breakpoint" },
  { "<leader>D", desc = "Debugger panel" },
}

return {
  {
    src = "mfussenegger/nvim-dap",
    keys = keys,
    cmd = { "DapContinue", "DapToggleBreakpoint", "DapTerminate" },
    config = function()
      local dap = require("dap")
      require("bud.dap.adapters").setup(dap, require("bud.lang").collect("dap"))
      local icons = require("bud.icons")
      vim.fn.sign_define("DapBreakpoint", { text = icons.breakpoint, texthl = "DiagnosticError" })
      vim.fn.sign_define("DapStopped", { text = icons.stopped, texthl = "DiagnosticWarn", linehl = "Visual" })
      local map = vim.keymap.set
      map("n", "<F5>", dap.continue, { desc = "Debug: start or continue" })
      map("n", "<F9>", dap.toggle_breakpoint, { desc = "Debug: toggle breakpoint" })
      map("n", "<leader>B", dap.toggle_breakpoint, { desc = "Toggle breakpoint" })
      map("n", "<F10>", dap.step_over, { desc = "Debug: step over" })
      map("n", "<F11>", dap.step_into, { desc = "Debug: step into" })
      map("n", "<S-F11>", dap.step_out, { desc = "Debug: step out" })
      map("n", "<leader>D", function()
        require("bud.pack").load("nvim-dap-view")
        require("dap-view").toggle()
      end, { desc = "Debugger panel" })
      require("bud.pack").load("nvim-dap-view")
    end,
  },
  {
    src = "igorlfs/nvim-dap-view",
    version = vim.version.range("1.*"),
    cmd = { "DapViewOpen", "DapViewClose", "DapViewToggle" },
    config = function()
      -- it needs nvim-dap, which may not be loaded yet (:DapViewOpen first)
      require("bud.pack").load("nvim-dap")
      require("dap-view").setup({ auto_toggle = true, windows = { position = "below", size = 0.3 } })
    end,
  },
}
