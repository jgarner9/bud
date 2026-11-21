-- OpenCode inside Neovim (D150, JGA-350). Not wired to bud.ai yet.

return {
  {
    src = "sudo-tee/opencode.nvim",
    -- no tags upstream, so a commit
    version = "636ac6214dcb6770f6966c8132d73e6abaf7595c",
    cmd = { "Opencode" },
    config = function()
      -- its default <leader>o... maps would collide with Aerial's <leader>o
      require("opencode").setup({ default_global_keymaps = false })
    end,
  },
}
