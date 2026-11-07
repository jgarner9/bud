-- The dashboard buddy's moves (JGA-155, JGA-181): each is a list of frames drawn on a
-- fixed canvas, so a move never changes the size of the art or moves the text
-- beside it. Pure data plus the drawing: it touches no buffer or timer;
-- bud.dashboard plays the moves and bud.dashboard.view lays out the canvas.
--
-- A sprite is a list of rows, spaces see-through. A frame places sprites on the
-- canvas and holds for some ticks (bud.dashboard's tick_ms each). The buddy
-- stands on the bottom row with headroom above for jumps and flips.

local M = {}

-- the canvas, in cells, and where the buddy stands (sprites sit at x + col, y + row)
M.width, M.height = 18, 7
local W = M.width
local X, Y = 6, 3

-- the neutral face: draw swaps it for the mood's (happy, update, sleepy)
local N = "o.o"

local function bot(face, o)
  o = o or {}
  return {
    " " .. (o.top or "___") .. " ",
    "[" .. (face or N) .. "]",
    (o.l or "/") .. "[_]" .. (o.r or "\\"),
    o.legs or " ] [ ",
  }
end

-- frame(hold, { sprite, x, y [, "fx"] }, ...); "fx" parts are effects
-- (sparks, notes, words), drawn in their own colour
local function frame(hold, ...)
  return { hold = hold, parts = { ... } }
end
local function stand(hold, face, o, x, y)
  return frame(hold, { bot(face, o), x or X, y or Y })
end
local function fx(s, x, y)
  return { { s }, x, y, "fx" }
end
local function burst(rows, x, y)
  return { rows, x, y, "fx" }
end

local S = {
  crouch = { " ___ ", "[>.<]", "/]_[\\" },
  arms_up = { "\\___/", "[O.O]", " [_] ", " ] [ " },
  side_l = { "       | ", "-[o.o][_]=", "       | " },
  upside = { " [ ] ", "\\[‾]/", "[o'o]", " ‾‾‾ " },
  side_r = { " |       ", "=[_][o.o]-", " |       " },
  tada = { "  ___  ", "\\[^.^]/", "  [_]  ", "  ] [  " },
  wave_a = { " ___  o", "[^.^]/", "/[_] ", " ] [ " },
  wave_b = { " ___ o", "[^.^]|", "/[_] ", " ] [ " },
}

-- names lists every move in the order defined; the dashboard shuffles it
M.names = {}
M.moves = {}
local function move(name, frames)
  M.moves[name] = frames
  table.insert(M.names, name)
end

move("wave", {
  stand(6, N),
  stand(3, "^.^"),
  frame(3, { S.wave_a, X, Y }),
  frame(3, { S.wave_b, X, Y }),
  frame(3, { S.wave_a, X, Y }),
  frame(3, { S.wave_b, X, Y }),
  frame(3, { S.wave_a, X, Y }),
  frame(3, { S.wave_b, X, Y }),
  stand(6, "^.^"),
})

move("backflip", {
  stand(6, N),
  frame(3, { S.crouch, X, Y + 1 }),
  frame(1, { S.arms_up, X, Y - 1 }),
  frame(1, { S.arms_up, X, Y - 2 }),
  frame(1, { S.side_l, X - 3, Y - 2 }),
  frame(2, { S.upside, X, Y - 3 }),
  frame(1, { S.side_r, X - 1, Y - 2 }),
  frame(1, { S.arms_up, X, Y - 1 }),
  frame(2, { S.crouch, X, Y + 1 }),
  frame(8, { S.tada, X - 1, Y }, fx("✦", X - 3, Y - 1), fx("✦", X + 7, Y)),
  frame(2, { S.tada, X - 1, Y }, fx("·", X - 3, Y - 1), fx("·", X + 7, Y)),
  stand(6, "^.^"),
})

do -- dance: sway side to side, arms and legs swapping, notes popping
  local frames = {}
  for i = 1, 6 do
    if i % 2 == 1 then
      table.insert(
        frames,
        frame(3, { bot("^o^", { l = "\\", r = "\\", legs = " / ] " }), X - 1, Y }, fx("♪", X + 7, Y - 1 - (i % 3)))
      )
    else
      table.insert(
        frames,
        frame(3, { bot("^o^", { l = "/", r = "/", legs = " [ \\ " }), X + 1, Y }, fx("♫", X - 3, Y - (i % 3)))
      )
    end
  end
  table.insert(frames, stand(6, "^.^"))
  move("dance", frames)
end

do -- spin: the face slides round the head, twice, then dizzy
  local faces = { N, " o.", "  o", "   ", "o  ", ".o " }
  local arms = { { "/", "\\" }, { " ", "\\" }, { " ", "\\" }, { "/", "\\" }, { "/", " " }, { "/", " " } }
  local frames = { stand(4, N) }
  for _ = 1, 2 do
    for i, f in ipairs(faces) do
      table.insert(frames, frame(1, { bot(f, { l = arms[i][1], r = arms[i][2] }), X, Y }, fx("~", X - 2, Y + 2)))
    end
  end
  table.insert(frames, stand(4, "@.@", nil, X - 1))
  table.insert(frames, stand(4, "@.@", nil, X + 1))
  table.insert(frames, stand(4, "@.@"))
  table.insert(frames, stand(4, N))
  move("spin", frames)
end

do -- bounce: a big hop, then a small one, squashing on each landing
  local up = { l = "\\", r = "/" }
  move("bounce", {
    stand(4, N),
    frame(2, { S.crouch, X, Y + 1 }),
    stand(1, "^o^", up, X, Y - 1),
    stand(1, "^o^", up, X, Y - 2),
    stand(2, "^o^", up, X, Y - 3),
    stand(1, "^o^", up, X, Y - 2),
    stand(1, "^o^", up, X, Y - 1),
    frame(2, { S.crouch, X, Y + 1 }),
    stand(1, "^o^", up, X, Y - 1),
    stand(2, "^o^", up, X, Y - 2),
    stand(1, "^o^", up, X, Y - 1),
    frame(2, { S.crouch, X, Y + 1 }),
    stand(4, "^.^"),
  })
end

move("jetpack", {
  stand(4, N),
  frame(2, { bot("O.O"), X, Y }, fx("*   *", X, Y + 3)),
  frame(2, { bot("O.O"), X, Y - 1 }, fx(" ' ' ", X, Y + 3)),
  frame(2, { bot("^.^"), X, Y - 2 }, fx(" * * ", X, Y + 2)),
  frame(2, { bot("^.^"), X, Y - 3 }, fx(" ' ' ", X, Y + 1)),
  frame(2, { bot("^.^"), X + 1, Y - 3 }, fx(" * * ", X + 1, Y + 1)),
  frame(2, { bot("^.^"), X + 2, Y - 2 }, fx(" ' ' ", X + 2, Y + 2)),
  frame(2, { bot("^.^"), X + 1, Y - 3 }, fx(" * * ", X + 1, Y + 1)),
  frame(2, { bot("^.^"), X, Y - 2 }, fx(" ' ' ", X, Y + 2)),
  frame(2, { bot("^.^"), X - 1, Y - 3 }, fx(" * * ", X - 1, Y + 1)),
  frame(2, { bot("^.^"), X, Y - 2 }, fx(" ' ' ", X, Y + 2)),
  frame(2, { bot(N), X, Y - 1 }, fx(" * * ", X, Y + 3)),
  frame(2, { bot(N), X, Y }, fx(".   .", X, Y + 3)),
  frame(2, { S.crouch, X, Y + 1 }),
  stand(4, "^.^"),
})

move("think", {
  stand(4, N),
  stand(3, "o.O", { top = "_*_" }),
  frame(3, { bot("o.O", { top = "_*_" }), X, Y }, fx("( )", X + 1, Y - 1)),
  frame(3, { bot("o.O", { top = "___" }), X, Y }, fx("(   )", X, Y - 2)),
  frame(3, { bot("O.o", { top = "_*_" }), X, Y }, fx("( )", X + 1, Y - 1), fx(".", X + 6, Y + 1)),
  frame(3, { bot("O.o", { top = "___" }), X, Y }, fx("(   )", X, Y - 2), fx(". .", X + 6, Y + 1)),
  frame(3, { bot("o.O", { top = "_*_" }), X, Y }, fx("( )", X + 1, Y - 1), fx(". . .", X + 6, Y + 1)),
  frame(8, { bot("^.^", { top = "_!_", r = "/" }), X, Y }, fx("✦", X + 2, Y - 2)),
  stand(4, "^.^"),
})

-- sleep is also all the buddy does after midnight
move("sleep", {
  frame(4, { bot("-.-"), X, Y }, fx("z", X + 6, Y)),
  frame(4, { bot("-.-"), X, Y }, fx("z", X + 6, Y), fx("Z", X + 7, Y - 1)),
  frame(4, { bot("-.-", { top = "_._" }), X, Y }, fx("z", X + 6, Y), fx("Z", X + 7, Y - 1), fx("Z", X + 9, Y - 2)),
  frame(4, { bot("-o-", { top = "_._" }), X, Y }, fx("Z", X + 7, Y - 1), fx("Z", X + 9, Y - 2), fx("z", X + 10, Y - 3)),
  frame(4, { bot("-.-"), X, Y }, fx("Z", X + 9, Y - 2), fx("z", X + 10, Y - 3)),
  frame(4, { bot("-.-"), X, Y }, fx("z", X + 10, Y - 3)),
})

move("hearts", {
  stand(4, N),
  stand(3, "^.^"),
  frame(3, { bot("♥.♥"), X, Y }, fx("♥", X + 6, Y)),
  frame(3, { bot("♥.♥"), X, Y }, fx("♥", X + 7, Y - 1), fx("♥", X - 1, Y)),
  frame(3, { bot("♥.♥"), X, Y }, fx("♥", X + 8, Y - 2), fx("♥", X - 2, Y - 1), fx("♥", X + 6, Y)),
  frame(3, { bot("♥.♥"), X, Y }, fx("♥", X + 9, Y - 3), fx("♥", X - 2, Y - 2), fx("♥", X + 7, Y - 1)),
  frame(3, { bot("^.^"), X, Y }, fx("♥", X - 3, Y - 3), fx("♥", X + 8, Y - 2)),
  frame(3, { bot("^.^"), X, Y }, fx("♥", X + 9, Y - 3)),
  stand(4, "^.^"),
})

do -- juggle: three balls loop over his head, hand to hand
  local arc = {
    { X - 1, Y + 2 },
    { X - 1, Y },
    { X, Y - 2 },
    { X + 2, Y - 3 },
    { X + 4, Y - 2 },
    { X + 5, Y },
    { X + 5, Y + 2 },
    { X + 2, Y + 3 },
  }
  local looks = { N, N, "O.o", "O.o", "o.O", "o.O", N, N }
  local frames = { stand(4, N) }
  for _ = 1, 3 do
    for i = 1, #arc do
      local parts = { { bot(looks[i], { l = i % 4 == 1 and "\\" or "/", r = i % 4 == 3 and "/" or "\\" }), X, Y } }
      for _, off in ipairs({ 0, 3, 6 }) do
        local p = arc[(i + off - 1) % #arc + 1]
        table.insert(parts, fx("o", p[1], p[2]))
      end
      table.insert(frames, frame(2, unpack(parts)))
    end
  end
  table.insert(frames, stand(4, "^.^"))
  move("juggle", frames)
end

move("glitch", {
  stand(4, N),
  stand(1, "#.%", nil, X + 1),
  stand(1, "%_#", { top = "_?_" }, X - 1),
  stand(1, N),
  stand(1, "@#!", { l = "|", r = "|" }, X + 1),
  stand(1, "&.$", { top = "‾‾‾" }, X - 1),
  stand(1, "#.%", nil, X + 1),
  stand(6, "   ", { top = "   " }),
  frame(4, { bot(".  "), X, Y }, fx("rebooting", X - 2, Y - 1)),
  frame(4, { bot(".. "), X, Y }, fx("rebooting", X - 2, Y - 1)),
  frame(4, { bot("...", { top = "_*_" }), X, Y }, fx("rebooting", X - 2, Y - 1)),
  stand(3, N),
  stand(5, "^.^"),
})

do -- weights: squat, lift to the chest, press overhead, drop it
  local bar = "o=====o"
  local press = { "\\___/", "[>.<]", " [_] ", " ] [ " }
  local proud = { "\\___/", "[^.^]", " [_] ", " ] [ " }
  move("weights", {
    frame(4, fx(bar, X - 1, Y + 3), { bot(N), X, Y }),
    frame(4, fx(bar, X - 1, Y + 3), { S.crouch, X, Y + 1 }),
    frame(4, fx(bar, X - 1, Y + 2), { bot(">.<"), X, Y }),
    frame(2, fx(bar, X - 1, Y + 1), { bot(">.<"), X, Y }),
    frame(2, fx(bar, X - 1, Y - 1), { press, X, Y }),
    frame(2, fx(bar, X - 1, Y - 1), { press, X - 1, Y }),
    frame(2, fx(bar, X - 1, Y - 1), { press, X, Y }),
    frame(8, fx(bar, X - 1, Y - 1), { proud, X, Y }, fx("✦", X - 3, Y), fx("✦", X + 7, Y)),
    frame(2, { bot("O.O"), X, Y }, fx(bar, X + 5, Y + 1)),
    frame(3, { bot("O.O"), X, Y }, fx(bar, X + 5, Y + 3), fx("*", X + 4, Y + 2)),
    stand(5, "^.^"),
  })
end

do -- fireworks: two rockets go up and burst while he points
  local function show(rx)
    local fs = {}
    local arms = rx > X and { r = "/" } or { l = "\\" }
    for y = Y + 2, Y - 2, -1 do
      table.insert(fs, frame(1, { bot(N, arms), X, Y }, fx("'", rx, y)))
    end
    local bursts = {
      { { " . ", ".*.", " . " }, rx - 1 },
      { { "\\|/", "-*-", "/|\\" }, rx - 1 },
      { { ". ' .", "'   '", ". ' ." }, rx - 2 },
      { { ".   .", "     ", ".   ." }, rx - 2 },
    }
    for i, b in ipairs(bursts) do
      table.insert(fs, frame(i == 2 and 3 or 2, { bot("O.O"), X, Y }, burst(b[1], b[2], 0)))
    end
    return fs
  end
  local frames = { stand(4, N) }
  vim.list_extend(frames, show(X + 8))
  table.insert(frames, stand(3, "^o^"))
  vim.list_extend(frames, show(X - 4))
  table.insert(frames, stand(6, "^o^", { l = "\\", r = "/" }))
  move("fireworks", frames)
end

do -- teleport: fizzles out, pops up across the canvas, fizzles back
  local function fizz(sprite, level)
    local out = {}
    for r, row in ipairs(sprite) do
      local chars = vim.fn.split(row, "\\zs")
      for i, ch in ipairs(chars) do
        if ch ~= " " then
          local odd = (i + r) % 2 == 1
          if level == 1 then
            chars[i] = odd and ":" or ch
          elseif level == 2 then
            chars[i] = ":"
          else
            chars[i] = odd and "." or " "
          end
        end
      end
      out[r] = table.concat(chars)
    end
    return out
  end
  local me = bot(N)
  local function gone(x)
    return {
      frame(2, { fizz(me, 1), x, Y, "fx" }),
      frame(2, { fizz(me, 2), x, Y, "fx" }),
      frame(2, { fizz(me, 3), x, Y, "fx" }),
      frame(3),
    }
  end
  local function back(x)
    return {
      frame(2, { fizz(me, 3), x, Y, "fx" }),
      frame(2, { fizz(me, 2), x, Y, "fx" }),
      frame(2, { fizz(me, 1), x, Y, "fx" }),
      stand(4, "O.O", nil, x),
      stand(4, "^.^", nil, x),
    }
  end
  local frames = { stand(4, N), stand(3, "-.-", { top = "_*_" }) }
  vim.list_extend(frames, gone(X))
  vim.list_extend(frames, back(X + 7))
  vim.list_extend(frames, gone(X + 7))
  vim.list_extend(frames, back(X))
  move("teleport", frames)
end

move("sneeze", {
  stand(4, N),
  stand(4, "o.O"),
  frame(4, { bot("O.O"), X, Y - 1 }, fx("ah", X + 6, Y)),
  frame(4, { bot("O.O"), X, Y - 1 }, fx("ah..", X + 6, Y)),
  frame(5, { bot("O o", { l = "\\", r = "/" }), X, Y - 1 }, fx("AAH..", X + 6, Y - 1)),
  frame(
    5,
    { bot(">o<", { top = "_!_", l = "\\", r = "/" }), X - 1, Y },
    fx("ACHOO!", X + 5, Y),
    fx("*", X + 5, Y + 2),
    fx("*", X + 7, Y + 1)
  ),
  frame(3, { bot("-.-"), X, Y }, fx("*", X + 9, Y + 2)),
  stand(6, "^_^", { r = "/" }),
})

do -- peekaboo: hides behind his hands, peeks, boo!
  local hide = { " ___ ", "[\\_/]", " [_] ", " ] [ " }
  local peek = { " ___ ", "[o_/]", " [_] ", " ] [ " }
  move("peekaboo", {
    stand(4, N),
    frame(6, { hide, X, Y }),
    frame(3, { peek, X, Y }),
    frame(4, { hide, X, Y }),
    frame(3, { peek, X, Y }),
    frame(5, { hide, X, Y }),
    frame(6, { bot("^O^", { l = "\\", r = "/" }), X, Y - 1 }, fx("boo!", X + 6, Y - 1)),
    stand(5, "^.^"),
  })
end

move("magic", {
  stand(4, N),
  stand(3, "o.O", { top = "_*_" }),
  frame(2, { bot("O.O", { top = "_ _" }), X, Y }, fx("*", X + 2, Y - 1)),
  frame(2, { bot("O.O", { top = "_ _" }), X, Y }, fx("*", X + 2, Y - 2)),
  frame(3, { bot("O.O", { top = "_ _", r = "/" }), X, Y }, fx("✦", X + 2, Y - 3)),
  frame(3, { bot("^.^", { r = "/" }), X, Y }, burst({ "· ✦ ·" }, X, Y - 3)),
  frame(3, { bot("^.^"), X, Y }, burst({ "·   ·", "  .  " }, X, Y - 3)),
  frame(3, { bot("^.^"), X, Y }, burst({ ".   .", "     ", "  .  " }, X - 1, Y - 2)),
  frame(3, { bot("^.^"), X, Y }, fx(".", X - 2, Y), fx(".", X + 6, Y)),
  stand(4, "^.^", { top = "_*_" }),
})

move("kickball", {
  frame(3, { bot(N), X, Y }, fx("o", W - 1, Y + 3)),
  frame(2, { bot(" o."), X, Y }, fx("o", W - 3, Y + 3)),
  frame(2, { bot(" o."), X, Y }, fx("o", W - 5, Y + 3)),
  frame(2, { bot(" o."), X, Y }, fx("o", X + 6, Y + 3)),
  frame(3, { bot(">.<", { legs = " ]  /" }), X, Y }, fx("o", X + 5, Y + 3)),
  frame(1, { bot("O.O"), X, Y }, fx("o", X + 7, Y + 1)),
  frame(1, { bot("O.O"), X, Y }, fx("o", X + 9, Y - 1)),
  frame(1, { bot("^.^"), X, Y }, fx("o", X + 11, Y - 3)),
  frame(6, { bot("^.^", { r = "/" }), X, Y }),
  frame(2, { bot(" o.", { r = "/" }), X, Y }),
  frame(2, { bot(N), X, Y }, fx("o", X + 2, Y - 3)),
  frame(1, { bot("O.O"), X, Y }, fx("o", X + 2, Y - 2)),
  frame(4, { bot("x.x", { top = "_o_" }), X, Y }, fx("✦", X - 1, Y)),
  frame(4, { bot("@.@"), X, Y }, fx("o", X + 6, Y + 3)),
  stand(5, "^_^"),
})

--- draw lays a frame (nil: standing still) out on the canvas, the neutral
--- face swapped for face. It returns M.height rows, each a list of
--- { text, kind } runs ("bot" or "fx") covering exactly M.width cells, and
--- how many cells fell off the canvas (none, in a well-made move).
function M.draw(f, face, sleepy)
  face = face or N
  if not f then
    f = sleepy and frame(1, { bot(face), X, Y }, fx("z", X + 7, Y + 1)) or stand(1, face)
  end
  local cells = {}
  for y = 1, M.height do
    cells[y] = {}
    for x = 1, M.width do
      cells[y][x] = { " " }
    end
  end
  local clipped = 0
  for _, p in ipairs(f.parts) do
    local sprite, px, py, kind = p[1], p[2], p[3], p[4] or "bot"
    for r, row in ipairs(sprite) do
      if kind == "bot" then
        row = row:gsub("%[" .. vim.pesc(N) .. "%]", "[" .. face .. "]")
      end
      for i, ch in ipairs(vim.fn.split(row, "\\zs")) do
        local x, y = px + i, py + r
        if ch ~= " " then
          if y >= 1 and y <= M.height and x >= 1 and x <= M.width then
            cells[y][x] = { ch, kind }
          else
            clipped = clipped + 1
          end
        end
      end
    end
  end
  local rows = {}
  for y, line in ipairs(cells) do
    local runs = {}
    for _, cell in ipairs(line) do
      local last = runs[#runs]
      if last and last[2] == cell[2] then
        last[1] = last[1] .. cell[1]
      else
        table.insert(runs, { cell[1], cell[2] })
      end
    end
    rows[y] = runs
  end
  return rows, clipped
end

return M
