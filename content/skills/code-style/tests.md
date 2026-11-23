# Tests

## Tests

- Tests run with no network and no credentials: fake the external CLIs on `PATH` (a fake `opencode`, `gh`, `git` remote), use real git in temp dirs, and point `HOME` and the XDG dirs at throwaway directories.
- Golden files for output, with an `-update` flag; review the diff before committing it.
- Name tests for the behavior, as a sentence: `TestChangesOutsideTheAllowedPathsAreNotPushed`.
- Go: swap package-level vars and restore them with `t.Cleanup`; helpers call `t.Helper()`; failure messages show what was expected and the context.
- A failing test blocks the install or release, never just warns.
- Bug fixes come with the test that would have caught them.
