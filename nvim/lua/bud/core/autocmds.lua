local group = vim.api.nvim_create_augroup("bud", { clear = true })

-- bud doctor's startup timing (nvim.StartupTime) sets BUD_STARTUP_PROBE: it
-- times the first screen and quits, so nothing that comes after it should run
local probe = vim.env.BUD_STARTUP_PROBE == "1"

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
    -- bare `bud` only: no files, nothing typed into the empty buffer, nothing
    -- piped in, and a screen to show it on (headless runs are scripts)
    local buf = vim.api.nvim_get_current_buf()
    local empty = vim.api.nvim_buf_get_name(buf) == ""
      and vim.api.nvim_buf_line_count(buf) == 1
      and vim.api.nvim_buf_get_lines(buf, 0, 1, false)[1] == ""
    local headless = vim.tbl_contains(vim.v.argv, "--headless")
    -- started from Bud.app: a cold start's dashboard makes way for a drop (JGA-186)
    if vim.env.BUD_APP then
      require("bud.app").setup()
    end
    -- bud sets BUD_OPEN=project for `bud <dir>`, which gets a layout, not the dashboard
    local project = vim.env.BUD_OPEN == "project"
    if not headless and not probe then
      -- after the first screen, like the panes (BASE.md 4.2)
      vim.schedule(function()
        require("bud.ai.claude").setup()
        require("bud.tools").setup()
        require("bud.settings").setup()
        require("bud.gen").setup()
        if vim.env.BUD_OPEN == "gen" then
          require("bud.gen").run(vim.env.BUD_GEN)
        end
      end)
    end
    -- bud tools gen (BUD_OPEN=gen) skips the dashboard; the generator starts above
    local gen = vim.env.BUD_OPEN == "gen"
    if vim.fn.argc() == 0 and empty and not headless and not project and not gen then
      require("bud.dashboard").open()
    elseif not headless and not probe and vim.env.BUD_BIN and (project or vim.fn.argc() > 0) then
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
    if not probe then
      require("bud.sessions").save()
    end
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

-- :BudUpdate runs bud update in a terminal, like u on the dashboard; its
-- arguments go to bud update (:BudUpdate --check)
vim.api.nvim_create_user_command("BudUpdate", function(o)
  require("bud.cli").terminal(vim.list_extend({ "update" }, o.fargs))
end, {
  nargs = "*",
  desc = "Update bud: install a newer release, sync the config, restore plugins",
  complete = function()
    return { "--check", "--channel" }
  end,
})
