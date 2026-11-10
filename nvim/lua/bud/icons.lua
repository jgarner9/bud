-- Nerd Font glyphs: bud's accent in the editor, alongside the robot (D37).
-- Only long-standing ones (Font Awesome, Octicons, Devicons), which every Nerd
-- Font has, all one cell wide. Add here rather than inventing symbols elsewhere,
-- and never use emoji.
local icons = {
  project = "", -- nf-fa-folder_open
  file = "", -- nf-fa-file_o
  recent = "", -- nf-fa-history
  new = "", -- nf-fa-plus
  scratchpad = "", -- nf-fa-pencil
  plugins = "", -- nf-fa-plug
  mason = "", -- nf-fa-wrench
  doctor = "", -- nf-fa-stethoscope
  update = "", -- nf-fa-refresh
  style = "", -- nf-fa-exchange
  quit = "", -- nf-fa-power_off
  projects = "", -- nf-fa-folder
  pinned = "", -- nf-oct-pin
  unpinned = "", -- nf-fa-folder
  branch = "", -- nf-dev-git_branch
  changed = "", -- nf-fa-pencil_square_o
  clean = "", -- nf-fa-check
  ahead = "", -- nf-fa-arrow_up
  behind = "", -- nf-fa-arrow_down
  error = "", -- nf-fa-times_circle
  warn = "", -- nf-fa-warning
  startup = "", -- nf-fa-bolt
  claude = "", -- nf-oct-hubot
  tip = "", -- nf-fa-lightbulb_o
  tool = "", -- nf-fa-wrench
  chat = "", -- nf-fa-comments
  settings = "", -- nf-fa-cog
  pr = "", -- nf-oct-git_pull_request
  review = "", -- nf-fa-eye
  todo = "", -- nf-fa-tasks
  clock = "", -- nf-fa-clock_o
  breakpoint = "", -- nf-fa-circle
  stopped = "", -- nf-fa-arrow_right
  modified = "", -- nf-fa-circle
  locked = "", -- nf-fa-lock
  info = "", -- nf-fa-info_circle
  cursor = "", -- nf-fa-map_marker
  lsp = "", -- nf-fa-cogs
  command = "", -- nf-fa-terminal
}

-- kinds: the glyph for each LSP completion / symbol kind, for the completion
-- menu and the breadcrumbs. A table of its own, so the one-cell-wide check over
-- the strings above skips it (the dashboard test checks these separately). Font
-- Awesome only, and the ones that already have a name above reuse it
icons.kinds = {
  Text = "\u{f031}", -- nf-fa
  Method = "\u{f1b2}", -- nf-fa
  Function = "\u{f121}", -- nf-fa
  Constructor = "\u{f0ad}", -- nf-fa
  Field = "\u{f02b}", -- nf-fa
  Variable = "\u{f069}", -- nf-fa
  Property = "\u{f013}", -- nf-fa
  Class = "\u{f1b3}", -- nf-fa
  Interface = "\u{f0e8}", -- nf-fa
  Struct = "\u{f0db}", -- nf-fa
  Module = "\u{f187}", -- nf-fa
  Unit = "\u{f080}", -- nf-fa
  Value = "\u{f292}", -- nf-fa
  Enum = "\u{f0ca}", -- nf-fa
  EnumMember = "\u{f0c9}", -- nf-fa
  Keyword = "\u{f084}", -- nf-fa
  Constant = icons.locked,
  Snippet = "\u{f0c4}", -- nf-fa
  Color = "\u{f1fc}", -- nf-fa
  File = icons.file,
  Reference = "\u{f0c1}", -- nf-fa
  Folder = icons.projects,
  Event = icons.startup,
  Operator = "\u{f1ec}", -- nf-fa
  TypeParameter = "\u{f12e}", -- nf-fa
}

return icons
