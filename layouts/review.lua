-- For reviewing changes: the changed files on the left, and the working tree's
-- diff opened in its own tab (close it with :DiffviewClose to get back here).
return {
  description = "Changed files and the working tree's diff",
  panes = {
    { kind = "git", side = "left", size = 40 },
  },
  diff = true,
}
