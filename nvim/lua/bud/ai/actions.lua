-- Inline actions (BASE.md 4, JGA-90): explain, refactor or write tests for a
-- selection, and draft a commit message, all on the project's session. The
-- answer shows in the chat pane; edits Claude proposes come as permission
-- diffs to accept or reject (bud.ai.permissions).

local claude = require("bud.ai.claude")

local M = {}

-- what each action asks; %s is where the selection is
M.prompts = {
  explain = "Explain what the code at %s does, briefly. Don't change anything.",
  refactor = "Refactor the code at %s to be clearer, keeping what it does. Make the edits.",
  tests = "Write tests for the code at %s, next to the project's existing tests and in their style. Make the edits.",
}

-- range is the visual selection, or the current line, as @path#La-b
function M.range()
  local name = vim.api.nvim_buf_get_name(0)
  if name == "" or vim.bo.buftype ~= "" then
    return nil
  end
  local first, last = vim.fn.line("v"), vim.fn.line(".")
  if not vim.fn.mode():match("[vV\22]") then
    first, last = last, last
  end
  first, last = math.min(first, last), math.max(first, last)
  local rel = vim.fn.fnamemodify(name, ":.")
  return first == last and ("%s#L%d"):format(rel, first) or ("%s#L%d-%d"):format(rel, first, last)
end

-- run sends one action for the selection and shows the pane
function M.run(action)
  local where = M.range()
  if not where then
    vim.notify("bud: put the cursor in a file first", vim.log.levels.INFO)
    return
  end
  vim.api.nvim_feedkeys(vim.keycode("<Esc>"), "n", false)
  local text = M.prompts[action]:format("@" .. where)
  if claude.send(text) then
    require("bud.chat.pane").open({ focus = false })
    local v = require("bud.chat.pane").view()
    if v then
      v:add({ role = "user", text = text })
    end
  end
end

local function git(args)
  local res = vim.system(vim.list_extend({ "git" }, args), { text = true }):wait()
  return res.code == 0 and res.stdout or nil
end

-- commit_prompt is what's asked for a commit message: the staged diff and
-- recent subjects, so it matches the project's style
function M.commit_prompt(diff, log)
  return table.concat({
    "Write a commit message for these staged changes, in the style of the recent commits below.",
    "Reply with only the message: a subject line, then a blank line and a short body if it needs one. No code fences.",
    "",
    "Recent commits:",
    log or "",
    "Staged diff:",
    "```diff",
    diff,
    "```",
  }, "\n")
end

-- commit drafts a commit message into the gitcommit buffer you're in (git
-- commit opening bud's editor), above the comments git put there
function M.commit()
  local buf = vim.api.nvim_get_current_buf()
  local diff = git({ "diff", "--cached" })
  if not diff or diff == "" then
    vim.notify("bud: nothing staged to write a message for", vim.log.levels.INFO)
    return
  end
  local s = claude.session()
  local unsub
  unsub = claude.on("done", function(d, session)
    if session.key ~= s.key then
      return
    end
    unsub()
    if not d.ok or not d.text or not vim.api.nvim_buf_is_valid(buf) then
      vim.notify("bud: no commit message came back", vim.log.levels.WARN)
      return
    end
    local msg = vim.split(vim.trim(d.text:gsub("^```%w*\n", ""):gsub("\n```$", "")), "\n", { plain = true })
    vim.api.nvim_buf_set_lines(buf, 0, 0, false, msg)
    vim.notify("bud: drafted the commit message; edit away, then :wq")
  end)
  if not claude.send(M.commit_prompt(diff, git({ "log", "--oneline", "-10" }))) then
    unsub()
    return
  end
  vim.notify("bud: Claude is drafting the commit message…")
end

function M.setup()
  for lhs, action in pairs({ ["<leader>ae"] = "explain", ["<leader>af"] = "refactor", ["<leader>at"] = "tests" }) do
    vim.keymap.set({ "n", "x" }, lhs, function()
      M.run(action)
    end, {
      desc = ({ explain = "Explain this", refactor = "Refactor this", tests = "Write tests for this" })[action],
    })
  end
  vim.api.nvim_create_autocmd("FileType", {
    group = vim.api.nvim_create_augroup("bud.ai.actions", { clear = true }),
    pattern = "gitcommit",
    callback = function(ev)
      vim.keymap.set("n", "<leader>am", M.commit, { buffer = ev.buf, desc = "Draft the commit message" })
    end,
  })
  vim.api.nvim_create_user_command(
    "BudCommitMessage",
    M.commit,
    { desc = "Draft a commit message from the staged diff" }
  )
end

return M
