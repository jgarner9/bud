-- C (D10). clangd lints (compiler warnings, plus clang-tidy when the project has
-- a .clang-tidy) and formats with clang-format's style, but on save only in
-- projects with a .clang-format: C codebases keep their own style. It finds
-- headers and flags from a compile_commands.json, or compile_flags.txt
return {
  filetypes = { "c" },
  treesitter = { "c" },
  lsp = {
    -- filetypes narrowed to C: C++ and Objective-C aren't bud languages (yet)
    clangd = { filetypes = { "c" } },
  },
  format_markers = { ".clang-format", "_clang-format" },
  mason = { "clangd" },
  system = { "cc" },
  root = { "compile_commands.json", "compile_flags.txt", ".clangd", "Makefile", "CMakeLists.txt", ".git" },
  dap = "codelldb",
}
