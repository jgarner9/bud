-- opencode.nvim as bud's IDE chat (D150, BASE D151, JGA-350).
--
-- bud keeps opencode.nvim's own keymaps off (its <leader>o prefix collides
-- with Aerial) and puts the few it needs under bud's <leader>a keys instead.
-- Each key runs an :Opencode subcommand, and the plugin's lazy command stub
-- loads it on first use, so nothing here touches opencode.nvim before then
-- and the first screen pays only for the keymaps.
--
--   local oc = require("bud.ai.opencode")
--   oc.run("diff open")   -- :Opencode diff open, or a notice saying what's missing
--   oc.setup_opts()       -- what plugins/ai.lua hands require("opencode").setup
--
-- Permission requests show in the chat output, answered there (1/2/3 or
-- j/k and <CR>) or with <leader>ay and <leader>an from any window. Edits are
-- written when OpenCode allows them and reviewed afterwards with <leader>ad,
-- the session diff, which can revert a prompt's changes (docs/backends.md).

local M = {}

-- the keys bud maps, each to an :Opencode subcommand
M.keys = {
  { lhs = "<leader>ad", sub = "diff open", desc = "Review the AI session's changes" },
  { lhs = "<leader>ap", sub = "models", desc = "Pick the AI model" },
  { lhs = "<leader>aS", sub = "session select", desc = "Pick an AI session" },
  { lhs = "<leader>ay", sub = "permission accept", desc = "Allow the AI's request once" },
  { lhs = "<leader>an", sub = "permission deny", desc = "Deny the AI's request" },
}

-- the executable opencode.nvim starts; a test can point it elsewhere
M.executable = "opencode"

-- on is true when ai.backend is opencode
function M.on()
  return require("bud.ai.agent").backend() == "opencode"
end

-- problem says why an :Opencode command can't run now, or nil when it can
function M.problem()
  if not M.on() then
    return "this key needs the opencode backend: bud config set ai.backend opencode"
  end
  if vim.fn.executable(M.executable) ~= 1 then
    return "opencode isn't on PATH: install the harness with bud install --device <name>, or run bud ai setup"
  end
  return nil
end

-- run runs :Opencode <sub>, or says what's missing. It returns true when it ran.
function M.run(sub)
  local why = M.problem()
  if why then
    vim.notify("bud: " .. why, vim.log.levels.WARN)
    return false
  end
  vim.cmd("Opencode " .. (sub or ""))
  return true
end

-- share adds the visual selection (or this line) to the chat's context
function M.share(first, last)
  local why = M.problem()
  if why then
    vim.notify("bud: " .. why, vim.log.levels.WARN)
    return false
  end
  vim.cmd(("%d,%dOpencode add_visual_selection"):format(first, last))
  return true
end

-- setup_opts is the table plugins/ai.lua passes to require("opencode").setup
function M.setup_opts()
  return {
    default_global_keymaps = false,
    opencode_executable = M.executable,
  }
end

-- map sets bud's keys; plugins/ai.lua calls it from the spec's init
function M.map()
  for _, k in ipairs(M.keys) do
    vim.keymap.set("n", k.lhs, function()
      M.run(k.sub)
    end, { desc = k.desc })
  end
end

return M
