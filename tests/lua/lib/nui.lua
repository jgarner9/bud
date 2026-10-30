-- Puts nui.nvim on the runtimepath for tests of bud.ui and what's built on it
dofile("tests/lua/lib/plugin.lua")("nui.nvim", "lua/nui/popup/init.lua", "BUD_TEST_NUI")
