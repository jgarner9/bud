// bud-unattended-deny: an OpenCode server plugin that rejects a permission
// ask nobody answers in time (Suite D99, D111).
//
// OpenCode 2.3.2 waits forever on an "ask" when no client replies: a server
// session whose client detached would sit on the prompt until someone
// attaches. This plugin answers each ask still pending after the timeout
// with "reject" and a message, so the model is told no and carries on
// without the action. It never answers "once" or "always": it only turns a
// wait into a denial, so it can't loosen an approval. An ask a client answers
// first is left alone.
//
// It is plain JavaScript with no imports, so it loads without installing
// @opencode/plugin; Plugin.define only returns its argument.

// TIMEOUT_MS is how long an ask may wait for an answer before it is rejected
export const TIMEOUT_MS = 10 * 60 * 1000

export const MESSAGE =
  "Denied: nobody answered this permission request within 10 minutes, so it was refused (Bud Suite D111). " +
  "Don't retry it; leave the action for the user and say what you would have done."

// create builds the plugin. timeoutMs and the timer functions are for tests
export function create({
  timeoutMs = TIMEOUT_MS,
  setTimer = setTimeout,
  clearTimer = clearTimeout,
  log = console.error,
} = {}) {
  return {
    id: "bud-unattended-deny",
    setup(ctx) {
      const timers = new Map()
      const controller = new AbortController()

      const reject = async (sessionID, requestID) => {
        timers.delete(requestID)
        try {
          await ctx.permission.reply({ sessionID, requestID, decision: "reject", message: MESSAGE })
        } catch (err) {
          // already answered, or the session is gone: nothing is waiting
          if (!controller.signal.aborted) log(`bud-unattended-deny: reject ${requestID}: ${err?.message ?? err}`)
        }
      }

      const watch = async () => {
        const events = ctx.event.subscribe({
          types: ["permission.asked", "permission.replied"],
          signal: controller.signal,
        })
        for await (const event of events) {
          if (controller.signal.aborted) break
          const data = event.data ?? {}
          if (event.type === "permission.asked" && data.id && !timers.has(data.id)) {
            timers.set(
              data.id,
              setTimer(() => void reject(data.sessionID, data.id), timeoutMs),
            )
          } else if (event.type === "permission.replied" && timers.has(data.requestID)) {
            clearTimer(timers.get(data.requestID))
            timers.delete(data.requestID)
          }
        }
      }
      watch().catch((err) => {
        if (!controller.signal.aborted) log(`bud-unattended-deny: event stream ended: ${err?.message ?? err}`)
      })

      // the stream ends on the abort; cleanup doesn't wait for it, so an
      // unload never hangs on the event stream
      return () => {
        controller.abort()
        for (const t of timers.values()) clearTimer(t)
        timers.clear()
      }
    },
  }
}

export default create()
