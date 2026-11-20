// This probe is appended to OpenCode 2.3.2's permission.test.ts by the runner.
it.effect("JGA-348: ordinary always reply satisfies later requests without review", () =>
  Effect.gen(function* () {
    yield* setup()
    const { service, fiber, request } = yield* waitForRequest({ save: ["src/*"] })
    yield* service.reply({ requestID: request.id, reply: "always" })
    yield* Fiber.join(fiber)
    yield* service.assert(assertion({ id: Permission.ID.create("per_jga348_next"), resources: ["src/next.ts"] }))
    expect(yield* service.list()).toEqual([])
  }),
)
