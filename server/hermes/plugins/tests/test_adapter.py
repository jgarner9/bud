"""Tests for bud-signal's adapter against stand-ins for the Hermes parts it uses (Hermes v0.23.2's
SignalAdapter, SendResult and tools.approval), so they run without Hermes installed."""

import asyncio
import importlib.util
import os
import sys
import types
import unittest
from dataclasses import dataclass
from typing import Any, Optional

PLUGIN = os.path.join(os.path.dirname(__file__), "..", "bud-signal")
BOT, JASON, OTHER = "+15559990000", "+15550001111", "+15552223333"
UP, DOWN = "\U0001F44D", "\U0001F44E"


@dataclass
class SendResult:
    success: bool
    message_id: Optional[str] = None
    error: Optional[str] = None
    raw_response: Any = None


@dataclass
class ExecApprovalPrompt:
    chat_id: str
    session_key: str
    text: str
    actions: list
    command: str = "rm -rf build"
    description: str = "asks"
    smart_denied: bool = False
    metadata: Optional[dict] = None
    request_id: Optional[str] = None


class FakeSignalAdapter:
    """What bud-signal calls on Hermes's SignalAdapter."""

    def __init__(self, config):
        self.config = config
        self.account = BOT
        self._account_normalized = BOT
        self.dm_allow_from = set(config.allow)
        self.sent, self.reactions, self.passed = [], [], []
        self.next_ts = 1700
        self.send_ok = True
        self.authorized = True

    async def send(self, chat_id, content, reply_to=None, metadata=None):
        self.sent.append((chat_id, content))
        if not self.send_ok:
            return SendResult(success=False, error="RPC send failed")
        self.next_ts += 1
        return SendResult(success=True, raw_response={"timestamp": self.next_ts})

    async def send_reaction(self, chat_id, emoji, target_author, target_timestamp):
        self.reactions.append((chat_id, emoji, target_author, target_timestamp))
        return True

    def _is_sender_authorized(self, user_id, chat_type=None, chat_id=None, **kw):
        return self.authorized

    async def _handle_envelope(self, envelope):
        self.passed.append(envelope)


class Approvals:
    """tools.approval's pending queue, as far as bud-signal sees it."""

    def __init__(self):
        self.pending = {}  # request_id -> (session_key, requester, deny_only)
        self.calls = []

    def resolve_gateway_approval_card(self, session_key, choice, request_id, *, actor_id=None, admin_ids=(),
                                      extra=None, admitted=True):
        self.calls.append((session_key, choice, request_id, actor_id))
        entry = self.pending.get(request_id)
        if entry is None or entry[0] != session_key:
            return 0
        if choice != "deny" and entry[2]:
            return 0
        if actor_id != entry[1]:
            return 0
        del self.pending[request_id]
        return 1

    def list_gateway_approvals(self, session_key):
        return [{"request_id": r} for r, e in self.pending.items() if e[0] == session_key]


def load(approvals):
    registered = {}
    base = types.ModuleType("gateway.platforms.base")
    base.SendResult = SendResult
    bea = types.ModuleType("gateway.platforms.base_exec_approval")
    bea.approval_timeout_seconds = lambda: 3600
    sig_adapter = types.ModuleType("plugins.platforms.signal.adapter")
    sig_adapter.SignalAdapter = FakeSignalAdapter
    sig_register = types.ModuleType("plugins.platforms.signal.register")

    def bundled_register(ctx):
        ctx.register_platform(name="signal", label="Signal", adapter_factory=FakeSignalAdapter, check_fn=bool,
                              cron_deliver_env_var="SIGNAL_HOME_CHANNEL")
    sig_register.register = bundled_register
    sig_pkg = types.ModuleType("plugins.platforms.signal")
    # as in Hermes v0.23.2, whose __init__ re-exports the function over the submodule
    sig_pkg.register = bundled_register
    tools_approval = types.ModuleType("tools.approval")
    tools_approval.resolve_gateway_approval_card = approvals.resolve_gateway_approval_card
    tools_approval.list_gateway_approvals = approvals.list_gateway_approvals
    for name in ["gateway", "gateway.platforms", "plugins", "plugins.platforms", "tools"]:
        sys.modules[name] = types.ModuleType(name)
    sys.modules.update({
        "gateway.platforms.base": base, "gateway.platforms.base_exec_approval": bea,
        "plugins.platforms.signal": sig_pkg, "plugins.platforms.signal.adapter": sig_adapter,
        "plugins.platforms.signal.register": sig_register, "tools.approval": tools_approval,
    })
    for name in [m for m in sys.modules if m.startswith("bud_signal")]:
        del sys.modules[name]
    spec = importlib.util.spec_from_file_location("bud_signal", os.path.join(PLUGIN, "__init__.py"),
                                                  submodule_search_locations=[PLUGIN])
    mod = importlib.util.module_from_spec(spec)
    sys.modules["bud_signal"] = mod
    spec.loader.exec_module(mod)

    class Ctx:
        manifest = "bud-signal"

        def register_platform(self, **kw):
            registered.update(kw)

    mod.register(Ctx())
    return registered


class Config:
    def __init__(self, allow):
        self.allow = allow
        self.extra = {"http_url": "http://127.0.0.1:8080"}


def run(coro):
    return asyncio.run(coro)


def reaction(emoji, ts, sender=JASON, target=BOT, remove=False, uuid=None):
    return {"envelope": {"sourceNumber": sender, "dataMessage": {"reaction": {
        "emoji": emoji, "targetAuthorNumber": target, "targetAuthorUuid": uuid, "targetSentTimestamp": ts,
        "isRemove": remove}}}}


def text(body, sender=JASON):
    return {"envelope": {"sourceNumber": sender, "dataMessage": {"message": body}}}


BOTH = [("Allow once", "once", "primary"), ("Deny", "deny", "danger")]


class AdapterTest(unittest.TestCase):
    def setUp(self):
        self.approvals = Approvals()
        self.registered = load(self.approvals)
        self.a = self.registered["adapter_factory"](Config([JASON]))

    def ask(self, request_id="r1", actions=BOTH, deny_only=False, command="rm -rf build", requester=JASON):
        self.approvals.pending[request_id] = ("sess", requester, deny_only)
        res = run(self.a._send_exec_approval_prompt(ExecApprovalPrompt(
            chat_id=JASON, session_key="sess", text="Hermes wants to run a command", actions=actions,
            command=command, request_id=request_id)))
        prompt = self.a._prompts.by_timestamp(int(res.message_id)) if res.message_id else None
        return res, prompt

    def test_it_takes_over_the_bundled_signal_platform_and_keeps_the_rest(self):
        self.assertEqual(self.registered["name"], "signal")
        self.assertEqual(self.registered["cron_deliver_env_var"], "SIGNAL_HOME_CHANNEL")
        self.assertEqual(self.registered["adapter_factory"].__name__, "BudSignalAdapter")

    def test_the_prompt_carries_its_code(self):
        res, p = self.ask()
        self.assertTrue(res.success)
        chat, body = self.a.sent[-1]
        self.assertEqual(chat, JASON)
        self.assertIn("Hermes wants to run a command", body)
        self.assertIn(f"yes {p.code}", body)

    def test_a_thumbs_up_approves_that_request_once(self):
        res, p = self.ask()
        run(self.a._handle_envelope(reaction(UP, p.timestamp)))
        self.assertEqual(self.approvals.calls, [("sess", "once", "r1", JASON)])
        self.assertEqual(self.a.reactions, [(JASON, "✅", BOT, p.timestamp)])
        self.assertEqual(self.a.passed, [])
        self.assertEqual(len(self.a._prompts), 0)

    def test_a_thumbs_down_denies_it(self):
        res, p = self.ask()
        run(self.a._handle_envelope(reaction(DOWN + "\U0001F3FE", p.timestamp)))
        self.assertEqual(self.approvals.calls, [("sess", "deny", "r1", JASON)])
        self.assertEqual(self.a.reactions[-1][1], "❌")

    def test_codes_answer_too(self):
        _, p1 = self.ask("r1")
        _, p2 = self.ask("r2")
        run(self.a._handle_envelope(text(f"no {p2.code}")))
        run(self.a._handle_envelope(text(f"Yes {p1.code}")))
        self.assertEqual(self.approvals.calls, [("sess", "deny", "r2", JASON), ("sess", "once", "r1", JASON)])
        self.assertEqual(self.a.passed, [])

    def test_an_unknown_code_is_said_so_and_not_passed_to_the_agent(self):
        run(self.a._handle_envelope(text("yes 0000")))
        self.assertEqual(self.a.sent[-1], (JASON, "No approval is waiting for code 0000."))
        self.assertEqual(self.a.passed, [])

    def test_strangers_and_other_reactions_answer_nothing(self):
        _, p = self.ask()
        for e in [reaction(UP, p.timestamp, sender=OTHER), reaction("❤️", p.timestamp),
                  reaction(UP, p.timestamp, remove=True), reaction(UP, p.timestamp + 99),
                  reaction(UP, p.timestamp, target=JASON), reaction(UP, p.timestamp, target=None, uuid="uuid-x"),
                  text(f"yes {p.code}", sender=OTHER), text("hello")]:
            run(self.a._handle_envelope(e))
        self.assertEqual(self.approvals.calls, [])
        self.assertEqual(len(self.a.passed), 8)
        self.assertIn("r1", self.approvals.pending)

    def test_the_gateways_own_check_must_say_yes(self):
        _, p = self.ask()
        self.a.authorized = None
        run(self.a._handle_envelope(reaction(UP, p.timestamp)))
        self.assertEqual(self.approvals.calls, [])

    def test_an_open_allowlist_lets_nobody_answer(self):
        a = self.registered["adapter_factory"](Config(["*"]))
        self.approvals.pending["r1"] = ("sess", JASON, False)
        res = run(a._send_exec_approval_prompt(ExecApprovalPrompt(
            chat_id=JASON, session_key="sess", text="t", actions=BOTH, request_id="r1")))
        run(a._handle_envelope(reaction(UP, int(res.message_id))))
        self.assertEqual(self.approvals.calls, [])

    def test_deny_only_prompts_refuse_an_approval(self):
        _, p = self.ask(actions=[("Deny", "deny", "danger")], deny_only=True)
        self.assertNotIn(f"yes {p.code}", self.a.sent[-1][1])
        run(self.a._handle_envelope(reaction(UP, p.timestamp)))
        self.assertEqual(self.approvals.calls, [])
        self.assertIn("can only be denied", self.a.sent[-1][1])
        run(self.a._handle_envelope(reaction(DOWN, p.timestamp)))
        self.assertEqual(self.approvals.calls, [("sess", "deny", "r1", JASON)])

    def test_a_late_answer_is_told(self):
        _, p = self.ask()
        del self.approvals.pending["r1"]  # timed out
        run(self.a._handle_envelope(reaction(UP, p.timestamp)))
        self.assertIn("isn't waiting any more", self.a.sent[-1][1])
        self.assertEqual(len(self.a._prompts), 0)

    def test_a_failed_send_keeps_no_prompt(self):
        self.a.send_ok = False
        res, _ = self.ask()
        self.assertFalse(res.success)
        self.assertEqual(len(self.a._prompts), 0)

    def test_without_a_request_id_it_leaves_the_prompt_to_typed_approve(self):
        res = run(self.a._send_exec_approval_prompt(ExecApprovalPrompt(
            chat_id=JASON, session_key="sess", text="t", actions=BOTH, request_id=None)))
        self.assertFalse(res.success)
        self.assertEqual(self.a.sent, [])


    def test_a_command_he_named_in_backticks_runs_once_without_asking(self):
        run(self.a._handle_envelope(text("please run `git push`")))
        self.assertEqual(len(self.a.passed), 1)  # his message still reaches the agent
        res, p = self.ask(command="git push")
        self.assertTrue(res.success)
        self.assertIsNone(p)
        self.assertEqual(self.approvals.calls, [("sess", "once", "r1", JASON)])
        self.assertIn("approved once by your message", self.a.sent[-1][1])
        _, p2 = self.ask("r2", command="git push")
        self.assertIsNotNone(p2)
        self.assertEqual(len(self.approvals.calls), 1)

    def test_anything_else_still_asks(self):
        run(self.a._handle_envelope(text("please run `git push`")))
        _, p = self.ask(command="git push --force")
        self.assertIsNotNone(p)
        self.assertEqual(self.approvals.calls, [])

    def test_his_next_message_replaces_what_it_covers(self):
        run(self.a._handle_envelope(text("please run `git push`")))
        run(self.a._handle_envelope(text("actually wait")))
        _, p = self.ask(command="git push")
        self.assertIsNotNone(p)

    def test_an_approval_reply_clears_previous_commands(self):
        run(self.a._handle_envelope(text("please run `git push`")))
        _, p = self.ask(command="another command")
        run(self.a._handle_envelope(text(f"yes {p.code}")))
        _, p = self.ask("r2", command="git push")
        self.assertIsNotNone(p)

    def test_a_strangers_backticks_cover_nothing(self):
        run(self.a._handle_envelope(text("run `git push`", sender=OTHER)))
        _, p = self.ask(command="git push")
        self.assertIsNotNone(p)
        self.assertEqual(self.approvals.calls, [])

    def test_a_deny_only_prompt_is_never_pre_approved(self):
        run(self.a._handle_envelope(text("run `git push`")))
        _, p = self.ask(actions=[("Deny", "deny", "danger")], deny_only=True, command="git push")
        self.assertIsNotNone(p)
        self.assertEqual(self.approvals.calls, [])

    def test_when_hermes_refuses_the_pre_approval_it_asks_instead(self):
        run(self.a._handle_envelope(text("run `git push`")))
        _, p = self.ask(command="git push", requester=OTHER)
        self.assertIsNotNone(p)
        self.assertIn(f"yes {p.code}", self.a.sent[-1][1])


if __name__ == "__main__":
    unittest.main()
