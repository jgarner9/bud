-- Migrations run once each, in order, during `bud update` (BASE.md 5.7 step 3),
-- after the new config is synced and before plugins are restored. Add one when
-- a release moves or renames something on disk that a sync alone won't fix.
--
-- Name: NNNN_short_name.lua, numbered one past the last. Never edit or renumber
-- one that has shipped: machines that ran it won't run it again.
--
-- Each runs as `nvim --clean --headless -l <file>`, so vim.fs and vim.uv are
-- there but bud's config isn't. bud's dirs come in as environment variables:
-- BUD_CONFIG_DIR, BUD_DATA_DIR, BUD_STATE_DIR and BUD_CACHE_DIR. Raise an error
-- to fail: the update stops, and the migration runs again next time.
--
-- A fresh install marks every migration as done without running it, but write
-- them to cope with things already being in their new place anyway.
--
-- This one does nothing.

local state = assert(os.getenv("BUD_STATE_DIR"), "BUD_STATE_DIR is unset")
assert(vim.fn.isdirectory(state) == 1, state .. " is missing")
