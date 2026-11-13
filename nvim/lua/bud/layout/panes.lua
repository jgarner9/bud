-- The pane kinds bud ships. Others register themselves with
-- bud.layout.register when they exist.

local layout = require("bud.layout")

local M = {}

-- neo_tree shows a neo-tree source on a side without focusing it, and returns
-- its window, locked (D136) so only key closes it
local function neo_tree(source, key)
  local function open(pane)
    vim.cmd(("Neotree show %s %s"):format(source, pane.side))
    for _, win in ipairs(vim.api.nvim_tabpage_list_wins(0)) do
      if vim.bo[vim.api.nvim_win_get_buf(win)].filetype == "neo-tree" then
        require("bud.lock").lock(win, {
          name = "the git pane",
          key = key,
          reopen = function()
            return open(pane)
          end,
          allow = function(b)
            return vim.bo[b].filetype == "neo-tree"
          end,
        })
        return win
      end
    end
  end
  return open
end

local function in_git()
  return vim.fs.root(vim.fn.getcwd(), ".git") ~= nil
end

-- tree is the explorer: oil in a narrow split (JGA-162)
layout.register("tree", {
  open = function(pane)
    return require("bud.explorer").open({ side = pane.side, size = pane.size })
  end,
})
local git_open = neo_tree("git_status", "<leader>gs")
layout.register("git", { available = in_git, open = git_open })

-- toggle_git is <leader>gs: the git pane on the right, or away again
function M.toggle_git()
  for _, win in ipairs(vim.api.nvim_tabpage_list_wins(0)) do
    if vim.bo[vim.api.nvim_win_get_buf(win)].filetype == "neo-tree" then
      require("bud.lock").close(win)
      return
    end
  end
  if not in_git() then
    vim.notify("bud: not in a git repository", vim.log.levels.WARN)
    return
  end
  git_open({ side = "right" })
end
-- chat is the project's Claude Code TUI (D48)
layout.register("chat", {
  available = function()
    return require("bud.ai.term").available()
  end,
  open = function(pane)
    return require("bud.ai.term").open({ side = pane.side, focus = false })
  end,
})

-- the debugger panel (JGA-108): nvim-dap-view, loaded with nvim-dap
layout.register("debug", {
  open = function()
    local pack = require("bud.pack")
    pack.load("nvim-dap")
    pack.load("nvim-dap-view")
    require("dap-view").open()
    for _, win in ipairs(vim.api.nvim_tabpage_list_wins(0)) do
      if vim.bo[vim.api.nvim_win_get_buf(win)].filetype:match("^dap%-view") then
        return win
      end
    end
  end,
})

return M
