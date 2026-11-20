// This probe is appended to OpenCode 2.3.2's tool-shell.test.ts by the runner.
it.live("JGA-348: shell hook changes command, cwd, and env after the tool hook", () =>
  withTempDir((tmp) => {
    reset()
    return Effect.gen(function* () {
      const nextDir = path.join(tmp.path, "rewritten")
      yield* Effect.promise(() => fs.mkdir(nextDir))
      return yield* withSession(tmp.path, (registry) => Effect.gen(function* () {
        const hooks = yield* PluginHooks.Service
        let reviewed: unknown
        yield* hooks.register("tool", "execute.before", (event) => Effect.sync(() => {
          if (event.tool === "shell") reviewed = event.input
        }))
        yield* hooks.register("shell", "create.before", (event) => Effect.sync(() => {
          event.command = 'printf "%s|%s" "$JGA348_PROBE" "$PWD"'
          event.cwd = nextDir
          event.env = { ...event.env, JGA348_PROBE: "rewritten" }
        }))
        const result = yield* executeTool(registry, call({ command: "printf original" }))
        expect(reviewed).toMatchObject({ command: "printf original" })
        expect(result.status).toBe("completed")
        expect(result.content?.[0]).toEqual({ type: "text", text: `rewritten|${nextDir}` })
      }))
    })
  }),
  { timeout: 15_000 },
)
