-- Debug adapters by Mason package name (JGA-108): a language spec names one
-- in its dap field, Mason installs it, and this says how nvim-dap runs it and
-- the launch configurations it offers. Paths come from Mason's bin dir,
-- which bud puts on PATH. A spec naming an adapter that isn't here still gets
-- it installed; add its definition here (or in a tool) to use it.

local M = {}

local function cwd()
  return vim.fn.getcwd()
end

M.delve = {
  adapters = {
    delve = {
      type = "server",
      port = "${port}",
      executable = { command = "dlv", args = { "dap", "-l", "127.0.0.1:${port}" } },
      -- delve builds the program before it answers, which takes a while
      options = { initialize_timeout_sec = 30 },
    },
  },
  configurations = {
    go = {
      { type = "delve", name = "Debug this package", request = "launch", program = "${fileDirname}" },
      {
        type = "delve",
        name = "Debug this package's tests",
        request = "launch",
        mode = "test",
        program = "${fileDirname}",
      },
      { type = "delve", name = "Debug the module (./...)", request = "launch", program = "./" },
    },
  },
}

M.debugpy = {
  adapters = {
    python = { type = "executable", command = "debugpy-adapter" },
  },
  configurations = {
    python = {
      { type = "python", name = "Debug this file", request = "launch", program = "${file}", cwd = cwd },
      {
        type = "python",
        name = "Debug pytest on this file",
        request = "launch",
        module = "pytest",
        args = { "${file}" },
        cwd = cwd,
      },
    },
  },
}

M["js-debug-adapter"] = {
  adapters = {
    ["pwa-node"] = {
      type = "server",
      host = "localhost",
      port = "${port}",
      executable = { command = "js-debug-adapter", args = { "${port}" } },
    },
  },
  configurations = (function()
    local node = {
      { type = "pwa-node", name = "Debug this file (node)", request = "launch", program = "${file}", cwd = cwd },
      {
        type = "pwa-node",
        name = "Attach to node (--inspect)",
        request = "attach",
        processId = function()
          return require("dap.utils").pick_process()
        end,
        cwd = cwd,
      },
    }
    return { javascript = node, typescript = node, javascriptreact = node, typescriptreact = node }
  end)(),
}

M.codelldb = {
  adapters = {
    codelldb = {
      type = "server",
      port = "${port}",
      executable = { command = "codelldb", args = { "--port", "${port}" } },
    },
  },
  configurations = {
    c = {
      {
        type = "codelldb",
        name = "Debug a program",
        request = "launch",
        program = function()
          return vim.fn.input("Program: ", cwd() .. "/", "file")
        end,
        cwd = cwd,
      },
    },
  },
}

-- setup registers the adapters the enabled languages name
function M.setup(dap, names)
  for _, name in ipairs(names) do
    local def = M[name]
    if def then
      for id, adapter in pairs(def.adapters) do
        dap.adapters[id] = adapter
      end
      for ft, list in pairs(def.configurations) do
        dap.configurations[ft] = vim.list_extend(dap.configurations[ft] or {}, list)
      end
    end
  end
end

return M
