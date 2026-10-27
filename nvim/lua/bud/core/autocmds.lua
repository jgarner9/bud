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
    local headless = vim.tbl_contains(vim.v.argv, "--headless")
    -- bud sets BUD_OPEN=project for `bud <dir>`, which gets a layout, not the dashboard
    local project = vim.env.BUD_OPEN == "project"
    if not headless then
      -- after the first screen, like the panes (BASE.md 4.2)
      vim.schedule(function()
        require("bud.ai.claude").setup()
      end)
    end
    if vim.env.BUD_OPEN == "chat" and not headless then
      -- bud chat: straight to full-screen chat
      vim.schedule(function()
        local resume = vim.env.BUD_CHAT_RESUME
        require("bud.chat").open({ resume = resume ~= "" and resume or nil })
      end)
    elseif vim.fn.argc() == 0 and empty and not headless and not project then
      require("bud.dashboard").open()
    elseif not headless and vim.env.BUD_BIN and (project or vim.fn.argc() > 0) then
      -- after the first screen, so panes never count against startup (D35)
      vim.schedule(function()
        require("bud.layout").open_project(vim.fn.getcwd(), { files = vim.fn.argc() > 0 })
      end)
    end
  end,
})

vim.api.nvim_create_autocmd("VimLeavePre", {
  group = group,
  desc = "Save the project's session (dashboard.sessions)",
  callback = function()
    require("bud.sessions").save()
  end,
})

vim.api.nvim_create_user_command("BudLayout", function(opts)
  local layout = require("bud.layout")
  if opts.args == "reload" then
    layout.reload()
  else
    layout.apply(opts.args ~= "" and opts.args or nil)
  end
end, {
  nargs = "?",
  desc = "Apply a pane layout (default: this project's), or reload the current one",
  complete = function()
    return vim.list_extend(require("bud.layout").names(), { "reload" })
  end,
})

vim.api.nvim_create_user_command("BudDashboard", function()
  require("bud.dashboard").open()
end, { desc = "Open the bud dashboard" })
