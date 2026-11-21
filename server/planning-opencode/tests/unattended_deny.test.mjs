// Tests for content/plugins/bud-unattended-deny (Suite D111), with a fake
// plugin context and fake timers: no OpenCode needed. Run with
// node --test server/planning-opencode/tests/
import assert from "node:assert/strict"
import test from "node:test"

import plugin, { create, MESSAGE, TIMEOUT_MS } from "../../../content/plugins/bud-unattended-deny/server.js"

// fakeContext is the part of OpenCode's plugin context the plugin uses: an
// event stream the test pushes into and a permission.reply that records
function fakeContext({ replyError } = {}) {
  const queue = []
  let wake
  let subscribed
  const replies = []
  const ctx = {
    event: {
      subscribe(options) {
        subscribed = options
        return {
          async *[Symbol.asyncIterator]() {
            while (!options.signal.aborted) {
              if (queue.length === 0) await new Promise((r) => (wake = r))
              while (queue.length > 0) yield queue.shift()
            }
          },
        }
      },
    },
    permission: {
      async reply(input) {
        replies.push(input)
        if (replyError) throw replyError
      },
    },
  }
  const push = (event) => {
    queue.push(event)
    wake?.()
  }
  return { ctx, push, replies, subscribed: () => subscribed, wakeUp: () => wake?.() }
}

function fakeTimers() {
  const pending = new Map()
  let next = 1
  return {
    setTimer(fn, ms) {
      const id = next++
      pending.set(id, { fn, ms })
      return id
    },
    clearTimer(id) {
      pending.delete(id)
    },
    pending,
    fireAll() {
      for (const [id, t] of [...pending]) {
        pending.delete(id)
        t.fn()
      }
    },
  }
}

// settle lets the plugin's event loop and replies run
const settle = () => new Promise((r) => setImmediate(r))

const asked = (id, sessionID = "ses_1") => ({
  type: "permission.asked",
  data: { id, sessionID, action: "linear_save_issue", resources: ["*"] },
})
const replied = (requestID, reply, sessionID = "ses_1") => ({
  type: "permission.replied",
  data: { requestID, sessionID, reply },
})

function start(opts = {}) {
  const f = fakeContext(opts)
  const timers = fakeTimers()
  const logs = []
  const p = create({ timeoutMs: 1000, ...timers, log: (m) => logs.push(m) })
  const cleanup = p.setup(f.ctx)
  return { ...f, timers, logs, cleanup }
}

test("the default export is the plugin, waiting ten minutes", () => {
  assert.equal(plugin.id, "bud-unattended-deny")
  assert.equal(typeof plugin.setup, "function")
  assert.equal(TIMEOUT_MS, 600000)
  assert.match(MESSAGE, /10 minutes/)
})

test("subscribes to asks and replies only, with an abort signal", () => {
  const s = start()
  assert.deepEqual(s.subscribed().types, ["permission.asked", "permission.replied"])
  assert.ok(s.subscribed().signal instanceof AbortSignal)
  s.cleanup()
})

test("an ask nobody answers is rejected with the message", async () => {
  const s = start()
  s.push(asked("per_1"))
  await settle()
  assert.equal(s.timers.pending.size, 1)
  assert.equal([...s.timers.pending.values()][0].ms, 1000)
  assert.deepEqual(s.replies, [])

  s.timers.fireAll()
  await settle()
  assert.deepEqual(s.replies, [{ sessionID: "ses_1", requestID: "per_1", decision: "reject", message: MESSAGE }])
  s.cleanup()
})

test("an ask a client answers first is left alone", async () => {
  for (const reply of ["once", "always", "reject"]) {
    const s = start()
    s.push(asked("per_1"))
    s.push(replied("per_1", reply))
    await settle()
    assert.equal(s.timers.pending.size, 0, reply)
    s.timers.fireAll()
    await settle()
    assert.deepEqual(s.replies, [], reply)
    s.cleanup()
  }
})

test("only ever replies reject", async () => {
  const s = start()
  for (let i = 0; i < 5; i++) s.push(asked(`per_${i}`, `ses_${i % 2}`))
  await settle()
  s.timers.fireAll()
  await settle()
  assert.equal(s.replies.length, 5)
  for (const r of s.replies) assert.equal(r.decision, "reject")
  s.cleanup()
})

test("a repeated ask event keeps one timer", async () => {
  const s = start()
  s.push(asked("per_1"))
  s.push(asked("per_1"))
  await settle()
  assert.equal(s.timers.pending.size, 1)
  s.cleanup()
})

test("a reply that fails is logged and nothing else happens", async () => {
  const s = start({ replyError: new Error("Permission request not found: per_1") })
  s.push(asked("per_1"))
  await settle()
  s.timers.fireAll()
  await settle()
  assert.equal(s.replies.length, 1)
  assert.equal(s.logs.length, 1)
  assert.match(s.logs[0], /reject per_1: Permission request not found/)
  s.cleanup()
})

test("cleanup aborts the stream and clears pending timers", async () => {
  const s = start()
  s.push(asked("per_1"))
  await settle()
  assert.equal(s.timers.pending.size, 1)
  s.cleanup()
  assert.equal(s.subscribed().signal.aborted, true)
  assert.equal(s.timers.pending.size, 0)
  s.wakeUp()
  await settle()
  assert.deepEqual(s.replies, [])
  assert.deepEqual(s.logs, [])
})

test("other events and asks without an id are ignored", async () => {
  const s = start()
  s.push({ type: "permission.asked", data: {} })
  s.push(replied("per_unknown", "once"))
  await settle()
  assert.equal(s.timers.pending.size, 0)
  s.cleanup()
})
