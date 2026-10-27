-- The pane kinds bud ships. Others register themselves with
-- bud.layout.register when they exist.

local layout = require("bud.layout")

-- neo_tree shows a neo-tree source on a side without focusing it, and returns
-- its window
local function neo_tree(source)
  return function(pane)
    vim.cmd(("Neotree show %s %s"):format(source, pane.side))
    for _, win in ipairs(vim.api.nvim_tabpage_list_wins(0)) do
      if vim.bo[vim.api.nvim_win_get_buf(win)].filetype == "neo-tree" then
        return win
      end
    end
  end
end

local function in_git()
  return vim.fs.root(vim.fn.getcwd(), ".git") ~= nil
end

layout.register("tree", { open = neo_tree("filesystem") })
layout.register("git", { available = in_git, open = neo_tree("git_status") })
-- chat is the native side pane (JGA-89), or Claude Code's own TUI with
-- ai.chat.pane = "term" (D48)
layout.register("chat", {
  available = function()
    return require("bud.ai.term").available()
  end,
  open = function(pane)
    if require("bud.config").get("ai.chat.pane", "native") == "term" then
      return require("bud.ai.term").open({ side = pane.side, focus = false })
    end
    return require("bud.chat.pane").open({ side = pane.side, size = pane.size, focus = false })
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
