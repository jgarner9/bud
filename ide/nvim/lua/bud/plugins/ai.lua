-- OpenCode inside Neovim (D150, JGA-350). bud.ai.opencode holds the keys and
-- the options; <leader>ac opens it.

return {
  {
    src = "sudo-tee/opencode.nvim",
    -- no tags upstream, so a commit; a new pin is reviewed as a diff of the
    -- plugin between the two commits before the lockfile moves (JGA-350)
    version = "636ac6214dcb6770f6966c8132d73e6abaf7595c",
    cmd = { "Opencode" },
    init = function()
      require("bud.ai.opencode").map()
    end,
    config = function()
      require("opencode").setup(require("bud.ai.opencode").setup_opts())
    end,
  },
}
