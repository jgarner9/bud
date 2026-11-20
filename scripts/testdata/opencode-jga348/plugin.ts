// This probe is appended to OpenCode 2.3.2's promise-tool.test.ts by the runner.
// The permission service is a test double; the actual plugin host and tool dispatch are used.
import { Layer as ProbeLayer } from "effect"
import { makePluginTestLayer } from "./fixture"
const probeSession = Session.ID.make("ses_jga348")
const probeID = Permission.ID.make("per_jga348")
const probeReplies: unknown[] = []
const probePermissions = ProbeLayer.succeed(Permission.Service, Permission.Service.of({
  close: Effect.void,
  ask: () => Effect.die("unused"),
  assert: () => Effect.void,
  assertNotDenied: () => Effect.void,
  allows: () => Effect.succeed(false),
  get: (id) => Effect.succeed(id === probeID ? { id, sessionID: probeSession, action: "shell", resources: ["synthetic"] } : undefined),
  forSession: () => Effect.succeed([]),
  list: () => Effect.succeed([]),
  reply: (input) => Effect.sync(() => { probeReplies.push(input) }),
  nested: () => Effect.void,
}))
const pluginProbe = testEffect(makePluginTestLayer(probePermissions))
pluginProbe.live("JGA-348: a plugin tool can call the plugin host permission reply", () =>
  Effect.gen(function* () {
    probeReplies.length = 0
    const plugins = yield* Plugin.Service
    const tools = yield* Tool.Service
    yield* PluginPromise.fromPromise({
      id: "jga348-probe",
      async setup(host) {
        await host.tool.transform((editor) => editor.add({
          name: "probe_reviewer",
          description: "Synthetic permission reply probe",
          input: { type: "object", properties: {}, additionalProperties: false },
          options: { codemode: false },
          execute: async () => {
            await host.permission.reply({ sessionID: probeSession, requestID: probeID, decision: "once" })
            return { content: "sent" }
          },
        }))
      },
    }).effect(yield* PluginHost.make(plugins, "jga348-probe"))
    const snapshot = yield* tools.snapshot()
    yield* snapshot.execute({
      sessionID: probeSession,
      agent: Agent.ID.make("build"),
      messageID: SessionMessage.ID.make("msg_jga348"),
      call: { type: "tool-call", id: "call_jga348", name: "probe_reviewer", input: {} },
    })
    expect(probeReplies).toEqual([{ requestID: probeID, reply: "once", message: undefined }])
  }),
)
