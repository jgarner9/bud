-- jk leaves Insert mode, and Terminal mode in bud's terminal panes (the
-- <leader>t terminal, Claude Code's pane and chat), with no lag on either
-- key: j goes in at once, and a k within M.window ms of it takes the j back
-- out (a backspace) and goes to Normal mode. Any other k is just a k. In a
-- terminal pane Normal mode is where the pane's toggle (<leader>t,
-- <leader>ac, <leader>ch) closes it and <leader>w moves to another window.
-- Esc is left alone, so in a terminal it reaches the program at once.

local M = {}

-- how soon after a j a k must come to count as jk, in ms
M.window = 200

-- now is the clock in ms; swappable in tests
M.now = function()
  return vim.uv.hrtime() / 1e6
end

-- the last key other than k, and when it came. on_key sees every key (typed,
-- mapped or replayed from a macro), but k itself is left out: when its
-- mapping runs, on_key may or may not have seen the k yet (Insert mode's
-- expr mapping runs first; Terminal mode's Lua one after), and only the key
-- before it matters
local last

local ns = vim.api.nvim_create_namespace("bud.escape")

local function track()
  vim.on_key(function(key, typed)
    if key ~= "k" and typed ~= "k" then
      last = { key = key, at = M.now() }
    end
  end, ns)
end

-- quick says whether this k follows a j closely enough to be jk
local function quick()
  return last ~= nil and last.key == "j" and M.now() - last.at <= M.window
end

-- insert_k is Insert mode's k: jk after a quick j, else a k. The line isn't
-- checked for the j: with keys waiting (fast typing, a macro) Neovim inserts
-- a run of them at once, so the j may not be in it yet, but the backspace
-- still comes after it
function M.insert_k()
  if quick() then
    last = nil
    return "<BS><Esc>"
  end
  return "k"
end

-- attach maps k in buf's Terminal mode (buffer-local, so fzf-lua's pickers,
-- which are terminals too, keep a plain k): after a quick j it sends the
-- program a backspace for the j and goes to Normal mode
function M.attach(buf)
  vim.keymap.set("t", "k", function()
    local chan = vim.bo[buf].channel
    if quick() then
      last = nil
      vim.fn.chansend(chan, "\127")
      vim.api.nvim_feedkeys(vim.keycode("<C-\\><C-n>"), "n", false)
    else
      vim.fn.chansend(chan, "k")
    end
  end, { buffer = buf, desc = "k; jk to Normal mode" })
end

function M.setup()
  track()
  vim.keymap.set("i", "k", M.insert_k, { expr = true, desc = "k; jk leaves Insert mode" })
end

-- _key records a key as on_key would, for tests: headless Neovim doesn't run
-- Terminal mode keys from feedkeys
function M._key(key)
  last = { key = key, at = M.now() }
end

return M
