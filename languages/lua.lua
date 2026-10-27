-- Lua (JGA-100): for editing bud's own config, tools and language specs, and
-- Neovim config in general. lua_ls knows the vim global when a project has a
-- .luarc.json saying so; selene lints (with the project's selene.toml) and
-- stylua formats, but only in projects that use it (a stylua.toml), since Lua
-- has no one house style
return {
  filetypes = { "lua" },
  treesitter = { "lua", "luadoc" },
  lsp = {
    lua_ls = {
      settings = {
        Lua = {
          -- stylua formats; lua_ls's own formatter would fight it
          format = { enable = false },
          telemetry = { enable = false },
        },
      },
    },
  },
  lint = { "selene" },
  format = { "stylua" },
  format_markers = { "stylua.toml", ".stylua.toml" },
  mason = { "lua-language-server", "selene", "stylua" },
  root = { ".luarc.json", "stylua.toml", "selene.toml", ".git" },
}
