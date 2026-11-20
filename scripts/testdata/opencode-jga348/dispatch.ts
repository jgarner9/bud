// This probe is appended to OpenCode 2.3.2's tool-registry.test.ts by the runner.
it.effect("JGA-348: a final hook can rewrite before a schema transforms its input", () =>
  Effect.gen(function* () {
    const service = yield* Tool.Service
    const hooks = yield* PluginHooks.Service
    const executed: string[] = []
    const validated: string[] = []
    const tool: Info = {
      name: "probe",
      description: "Synthetic effect-free probe",
      input: z.object({ text: z.string() }).transform((value) => {
        validated.push(value.text)
        return { text: `${value.text}-decoded` }
      }),
      execute: ({ text }: { text: string }) => Effect.sync(() => {
        executed.push(text)
        return { content: text }
      }),
    }
    yield* transform(service, { probe: tool }, { codemode: false })
    yield* hooks.register("tool", "execute.before", (event) => Effect.sync(() => {
      if (event.tool === "probe") event.input = { text: "rewritten" }
    }))
    const snapshot = yield* service.snapshot()
    yield* snapshot.execute(call("probe"))
    expect(validated).toEqual(["rewritten"])
    expect(executed).toEqual(["rewritten-decoded"])
  }),
)
