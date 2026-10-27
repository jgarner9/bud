-- For debugging (JGA-108): the file tree on the left and the debugger panel
-- along the bottom (scopes, watches, breakpoints, the REPL), which fills in
-- once a session starts with F5.
return {
  description = "File tree and the debugger panel; F5 starts debugging",
  panes = {
    { kind = "tree", side = "left", size = 30 },
    { kind = "debug", side = "bottom", size = 0.3 },
  },
}
