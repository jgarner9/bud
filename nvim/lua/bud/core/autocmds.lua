local group = vim.api.nvim_create_augroup("bud", { clear = true })

vim.api.nvim_create_autocmd("TextYankPost", {
  group = group,
  desc = "Briefly highlight yanked text",
  callback = function()
    vim.hl.on_yank()
  end,
})

vim.api.nvim_create_autocmd("VimEnter", {
  group = group,
  desc = "Show the dashboard for bare bud",
  callback = function()
    if vim.g.bud_start_ns then
      vim.g.bud_startup_ms = math.floor((vim.uv.hrtime() - vim.g.bud_start_ns) / 1e6)
    end
    -- bare `bud` only: no files, nothing typed into the empty buffer, nothing
    -- piped in, and a screen to show it on (headless runs are scripts)
    local buf = vim.api.nvim_get_current_buf()
    local empty = vim.api.nvim_buf_get_name(buf) == ""
      and vim.api.nvim_buf_line_count(buf) == 1
      and vim.api.nvim_buf_get_lines(buf, 0, 1, false)[1] == ""
    if vim.fn.argc() == 0 and empty and not vim.tbl_contains(vim.v.argv, "--headless") then
      require("bud.dashboard").open()
    end
  end,
})

vim.api.nvim_create_autocmd("VimLeavePre", {
  group = group,
  desc = "Save the project's session (dashboard.sessions)",
  callback = function()
    require("bud.dashboard").save_session()
  end,
})

vim.api.nvim_create_user_command("BudDashboard", function()
  require("bud.dashboard").open()
end, { desc = "Open the bud dashboard" })
