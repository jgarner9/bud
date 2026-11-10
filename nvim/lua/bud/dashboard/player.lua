-- Plays the buddy's moves (bud.dashboard.moves) one after another on a timer
-- (JGA-155, JGA-181), for whoever draws him: the dashboard and the corner
-- buddy (bud.buddy). It owns the timer and the order of the moves and draws
-- nothing itself.

local moves = require("bud.dashboard.moves")

local M = {}

-- LuaJIT's math.random starts from the same seed every time; seeded once
-- here for the shuffle and for the dashboard's tagline
math.randomseed(vim.uv.hrtime())

-- one tick is how long a frame's hold counts in, pause is the breath between
-- moves; swapped in tests
M.tick_ms = 80
M.pause_ms = 640

-- order is every move shuffled on first use, then repeated as is, so the
-- first move is a random one and each comes round once per cycle; at is the
-- one last played. Kept for the whole nvim and shared by every player, so
-- reopening the dashboard carries on the cycle
M.order, M.at = nil, 0

--- next_move returns the name of the next move in the cycle.
function M.next_move()
  if not M.order then
    M.order = vim.deepcopy(moves.names)
    for i = #M.order, 2, -1 do
      local j = math.random(i)
      M.order[i], M.order[j] = M.order[j], M.order[i]
    end
    M.at = 0
  end
  M.at = M.at % #M.order + 1
  return M.order[M.at]
end

--- start plays moves until the function it returns is called. opts.show(frame)
--- draws a frame, or the buddy standing still for nil; opts.ready() (optional)
--- says whether to start a move now, asked again a pause later when it says
--- no; opts.sleepy() (optional) true makes the next move a snore.
function M.start(opts)
  local timer = vim.uv.new_timer()
  local stopped = false
  local function after(ms, fn)
    timer:start(
      ms,
      0,
      vim.schedule_wrap(function()
        if not stopped then
          fn()
        end
      end)
    )
  end
  local idle
  local function play(frames, i)
    opts.show(frames[i])
    after(frames[i].hold * M.tick_ms, function()
      if i < #frames then
        return play(frames, i + 1)
      end
      opts.show(nil)
      idle()
    end)
  end
  idle = function()
    after(M.pause_ms, function()
      if opts.ready and not opts.ready() then
        return idle()
      end
      if opts.sleepy and opts.sleepy() then
        return play(moves.moves.sleep, 1)
      end
      play(moves.moves[M.next_move()], 1)
    end)
  end
  idle()
  return function()
    if not stopped then
      stopped = true
      timer:stop()
      timer:close()
    end
  end
end

return M
