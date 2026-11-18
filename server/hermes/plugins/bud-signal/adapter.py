"""Hermes's Signal adapter with approvals answered by reaction or code (Suite D30).

Hermes v0.23.2's Signal adapter has no approval buttons, so an approval arrives as text to be
answered with a typed /approve, and an incoming reaction is dropped as an empty message. This
subclass renders the approval the way a button platform does (the gateway then hands it the
request id and only that request can be answered from it) and adds the code and the reaction
lines under it. A thumbs-up on the prompt, or "yes <code>", approves that one call; a thumbs-down,
or "no <code>", denies it. Only a sender SIGNAL_ALLOWED_USERS names may answer, and Hermes checks
again that it's the person whose message raised the approval (approvals.approvers is empty).
Typed /approve and /deny keep working as before.

A command Jason puts in backticks in his own Signal message, or on a line that starts "run:", is
pre-approved: when the agent then asks to run exactly that command, the prompt approves itself
once and says so. bud's gate raises most of these, and Hermes shows them with a label for the
command, so the command is read from the gate's reason (approvals.command_of). His next message replaces what it covers, and text that reaches the agent any other way (tool output, web pages,
files, mail) never counts, since only his messages from his own number are read for it.

It relies on SignalAdapter's private parts (_handle_envelope, send's raw_response, the bundled
plugin's register), so plugin.yaml pins the Hermes version it was checked against.
"""

from __future__ import annotations

import logging
from typing import Any, Optional

from gateway.platforms.base import SendResult
# the package re-exports register.register, so import the module by its own path
from plugins.platforms.signal.register import register as bundled_register
from plugins.platforms.signal.adapter import SignalAdapter

from .approvals import (APPROVE, APPROVED_MARK, DENIED_MARK, GRACE_SECONDS, PreApprovals, Prompts, command_of,
                        footer, may_answer, parse_code, reaction_choice, reaction_of, text_of)

logger = logging.getLogger(__name__)


def _timeout() -> int:
    from gateway.platforms.base_exec_approval import approval_timeout_seconds
    return approval_timeout_seconds()


class BudSignalAdapter(SignalAdapter):
    """SignalAdapter whose approval prompts are answered by reaction or code."""

    def __init__(self, config):
        super().__init__(config)
        self._prompts = Prompts(ttl=_timeout() + GRACE_SECONDS)
        self._asked = PreApprovals(ttl=_timeout())
        if not any(may_answer(u, self.dm_allow_from) for u in self.dm_allow_from):
            logger.warning("bud-signal: SIGNAL_ALLOWED_USERS names nobody, so no Signal reaction or code "
                           "can answer an approval; set it to your own number")

    async def _send_exec_approval_prompt(self, prompt) -> SendResult:
        if not prompt.request_id:
            # an answer couldn't be tied to one request, so leave it to the typed /approve prompt
            return SendResult(success=False, error="no request id")
        deny_only = not any(choice != "deny" for _, choice, _ in prompt.actions)
        command = command_of(prompt.command, prompt.description)
        if not deny_only and command is not None and self._asked.take(prompt.chat_id, command):
            done = await self._approve_asked(prompt, command)
            if done is not None:
                return done
        p = self._prompts.reserve(prompt.session_key, prompt.request_id, prompt.chat_id, deny_only)
        result = await self.send(prompt.chat_id, f"{prompt.text}\n\n{footer(p.code, deny_only)}",
                                 metadata=prompt.metadata)
        if not result.success:
            self._prompts.drop(p)
            return result
        raw = result.raw_response if isinstance(result.raw_response, dict) else {}
        ts = raw.get("timestamp")
        if isinstance(ts, int) and ts > 0:
            self._prompts.bind(p, ts)
        else:
            logger.warning("bud-signal: signal-cli sent no timestamp for an approval, so only its code answers it")
        return SendResult(success=True, message_id=str(ts) if ts else None, raw_response=result.raw_response)

    async def _approve_asked(self, prompt, command: str) -> Optional[SendResult]:
        """Approve once a call Jason named in his message and tell him so; None when Hermes refuses
        it (it isn't his request, or it's gone), so the caller asks as usual."""
        from tools.approval import resolve_gateway_approval_card
        if not resolve_gateway_approval_card(prompt.session_key, APPROVE, prompt.request_id,
                                             actor_id=prompt.chat_id, extra=self.config.extra or {}):
            return None
        result = await self.send(prompt.chat_id, f"{APPROVED_MARK} Running `{command.strip()}`, "
                                 "approved once by your message.", metadata=prompt.metadata)
        return SendResult(success=True, message_id=None, raw_response=result.raw_response)

    async def _handle_envelope(self, envelope: dict) -> None:
        try:
            if await self._answer(envelope):
                return
        except Exception:
            # an answer that failed answers nothing: the approval stays pending until it times out
            logger.exception("bud-signal: couldn't answer an approval")
        await super()._handle_envelope(envelope)

    async def _answer(self, envelope: dict) -> bool:
        """Answer an approval from a reaction or code; True when the event was one and is used up."""
        reaction = reaction_of(envelope)
        if reaction is not None:
            if reaction.remove or not self._allowed(reaction.sender):
                return False
            choice = reaction_choice(reaction.emoji)
            p = self._prompts.by_timestamp(reaction.target_timestamp)
            if choice is None or p is None or not self._targets_us(reaction):
                return False
            await self._settle(p, choice, reaction.sender)
            return True
        text = text_of(envelope)
        if text is None or not self._allowed(text.sender):
            return False
        self._asked.record(text.sender, text.text)
        parsed = parse_code(text.text)
        if parsed is None:
            # his message to the agent: what it names in backticks or on a run: line is pre-approved for this turn
            return False
        choice, code = parsed
        p = self._prompts.by_code(code)
        if p is None:
            await self.send(text.sender, f"No approval is waiting for code {code}.")
            return True
        await self._settle(p, choice, text.sender)
        return True

    def _allowed(self, sender: str) -> bool:
        return (may_answer(sender, self.dm_allow_from)
                and self._is_sender_authorized(sender, "dm", sender) is True)

    def _targets_us(self, reaction) -> bool:
        # the bot's number is all it knows of itself, so a reaction naming its target by UUID alone
        # can't be shown to point at the bot's message and answers nothing
        return bool(reaction.target_number) and reaction.target_number == self._account_normalized

    async def _settle(self, p, choice: str, sender: str) -> None:
        from tools.approval import list_gateway_approvals, resolve_gateway_approval_card
        if p.chat_id != sender:
            # a prompt sent to one chat is answered from that chat only
            return
        if choice == APPROVE and p.deny_only:
            await self.send(p.chat_id, "That one can only be denied, because the whole command couldn't be shown.")
            return
        n = resolve_gateway_approval_card(p.session_key, choice, p.request_id, actor_id=sender,
                                          extra=self.config.extra or {})
        still = any(a.get("request_id") == p.request_id for a in list_gateway_approvals(p.session_key))
        if n:
            self._prompts.drop(p)
            if p.timestamp:
                await self.send_reaction(p.chat_id, APPROVED_MARK if choice == APPROVE else DENIED_MARK,
                                         self.account, p.timestamp)
        elif still:
            await self.send(p.chat_id, "Only the person whose message asked for this can answer it.")
        else:
            self._prompts.drop(p)
            await self.send(p.chat_id, "That approval isn't waiting any more: it was answered or it timed out.")


class _Swap:
    """The plugin context the bundled Signal plugin registers through, with its adapter swapped
    for ours, so every other part of the platform (setup, sends from cron, auth env) stays Hermes's."""

    def __init__(self, ctx: Any):
        self._ctx = ctx

    def register_platform(self, *args: Any, **kwargs: Any):
        kwargs["adapter_factory"] = BudSignalAdapter
        return self._ctx.register_platform(*args, **kwargs)

    def __getattr__(self, name: str) -> Any:
        return getattr(self._ctx, name)


def register(ctx: Any) -> None:
    bundled_register(_Swap(ctx))
