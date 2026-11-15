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
  Text = "", -- nf-fa
  Method = "", -- nf-fa
  Function = "", -- nf-fa
  Constructor = "", -- nf-fa
  Field = "", -- nf-fa
  Variable = "", -- nf-fa
  Property = "", -- nf-fa
  Class = "", -- nf-fa
  Interface = "", -- nf-fa
  Struct = "", -- nf-fa
  Module = "", -- nf-fa
  Unit = "", -- nf-fa
  Value = "", -- nf-fa
  Enum = "", -- nf-fa
  EnumMember = "", -- nf-fa
  Keyword = "", -- nf-fa
  Constant = icons.locked,
  Snippet = "", -- nf-fa
  Color = "", -- nf-fa
  File = icons.file,
  Reference = "", -- nf-fa
  Folder = icons.projects,
  Event = icons.startup,
  Operator = "", -- nf-fa
  TypeParameter = "", -- nf-fa
}

return icons
