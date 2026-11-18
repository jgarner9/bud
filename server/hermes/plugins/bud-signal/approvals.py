"""Signal approvals by reaction or code (Suite D30, D42): the parts that need no Hermes.

An approval prompt bud-signal posts on Signal carries a four-digit code. Jason answers it by
reacting to that message, a thumbs-up to approve it once and a thumbs-down to deny it, or by
replying "yes <code>" or "no <code>". This module keeps the prompts that are waiting, reads
reactions and codes out of signal-cli envelopes and words the prompt's last lines. adapter.py
wires it into Hermes's Signal adapter. Standard library only, so its tests run anywhere.
"""

from __future__ import annotations

import re
import secrets
import time
from dataclasses import dataclass, field
from typing import Callable, Optional

APPROVE = "once"  # Hermes's word for approving one call; a reaction never approves more
DENY = "deny"

THUMBS_UP = "\U0001F44D"
THUMBS_DOWN = "\U0001F44E"
APPROVED_MARK = "✅"
DENIED_MARK = "❌"

# what a client may add to the thumbs: skin tones and the emoji presentation selector
_MODIFIERS = re.compile("[\U0001F3FB-\U0001F3FF️]")
_CODE = re.compile(r"^\s*(yes|no)\s+(\d{4})\s*[.!]?\s*$", re.IGNORECASE)

# a prompt is kept this long past the approval timeout, so a late answer is told it came too late
GRACE_SECONDS = 600

# a command Jason puts in backticks: `cmd` or a ```fenced``` block (its first line may name a language),
# or a line of its own starting "run:", where the rest of the line is the command (no backticks to type)
_FENCED = re.compile(r"```[^\n`]*\n(.*?)```", re.DOTALL)
_INLINE = re.compile(r"`([^`\n]+)`")
_RUN = re.compile(r"^[ \t]*run:(.*)$", re.IGNORECASE | re.MULTILINE)


def reaction_choice(emoji: Optional[str]) -> Optional[str]:
    """The choice a reaction's emoji stands for, or None for any other emoji."""
    bare = _MODIFIERS.sub("", emoji or "")
    if bare == THUMBS_UP:
        return APPROVE
    if bare == THUMBS_DOWN:
        return DENY
    return None


def parse_code(text: Optional[str]) -> Optional[tuple[str, str]]:
    """``(choice, code)`` for a reply that is only "yes 1234" or "no 1234", else None."""
    m = _CODE.match(text or "")
    if not m:
        return None
    return (APPROVE if m.group(1).lower() == "yes" else DENY), m.group(2)


def commands_in(text: Optional[str]) -> list[str]:
    """The commands a message names in backticks or on a "run:" line, stripped: fenced blocks,
    then run lines, then inline backticks. Only those count, so a request in plain words never
    pre-approves anything. A "run:" line inside a fenced block is part of that block's command."""
    text = text or ""
    found = [m.strip() for m in _FENCED.findall(text)]
    rest = _FENCED.sub(" ", text)
    found += [m.strip() for m in _RUN.findall(rest)]
    found += [m.strip() for m in _INLINE.findall(rest)]
    return [c for c in found if c]


def footer(code: str, deny_only: bool) -> str:
    """The lines under a prompt saying how to answer it."""
    if deny_only:
        return (f"Only a deny works on this one, because the whole command couldn't be shown: "
                f"react {THUMBS_DOWN} or reply \"no {code}\".")
    return (f"React {THUMBS_UP} to approve it once or {THUMBS_DOWN} to deny it, "
            f"or reply \"yes {code}\" or \"no {code}\".")


@dataclass(frozen=True)
class Reaction:
    """A reaction from a direct message, as signal-cli reports it."""

    sender: str
    emoji: str
    target_number: Optional[str]
    target_uuid: Optional[str]
    target_timestamp: int
    remove: bool


@dataclass(frozen=True)
class Text:
    """A direct message's text and who sent it."""

    sender: str
    text: str


def _direct_message(event: dict) -> Optional[tuple[str, dict]]:
    """``(sender, dataMessage)`` for a direct message from someone else. Sync messages (the bot
    account's own devices) and group messages are never answers."""
    data = event.get("envelope", event) if isinstance(event, dict) else None
    if not isinstance(data, dict) or "syncMessage" in data:
        return None
    sender = data.get("sourceNumber") or data.get("sourceUuid") or data.get("source")
    message = data.get("dataMessage")
    if not sender or not isinstance(message, dict) or message.get("groupInfo"):
        return None
    return str(sender), message


def reaction_of(event: dict) -> Optional[Reaction]:
    """The reaction a signal-cli event carries, or None."""
    found = _direct_message(event)
    if found is None:
        return None
    sender, message = found
    r = message.get("reaction")
    if not isinstance(r, dict):
        return None
    try:
        ts = int(r.get("targetSentTimestamp") or 0)
    except (TypeError, ValueError):
        return None
    if ts <= 0:
        return None
    return Reaction(sender=sender, emoji=str(r.get("emoji") or ""),
                    target_number=r.get("targetAuthorNumber") or None,
                    target_uuid=r.get("targetAuthorUuid") or None,
                    target_timestamp=ts, remove=bool(r.get("isRemove")))


def text_of(event: dict) -> Optional[Text]:
    """The text of a direct message, or None."""
    found = _direct_message(event)
    if found is None:
        return None
    sender, message = found
    text = message.get("message")
    if not isinstance(text, str) or not text:
        return None
    return Text(sender=sender, text=text)


def may_answer(sender: str, allowed: set[str]) -> bool:
    """Only a sender the allowlist names may answer (Suite D30). An empty list or "*" names
    nobody, so a missing SIGNAL_ALLOWED_USERS fails closed."""
    return bool(sender) and "*" not in allowed and sender in allowed


@dataclass
class Prompt:
    """One approval prompt posted on Signal and not answered yet."""

    session_key: str
    request_id: str
    chat_id: str
    code: str
    deny_only: bool
    created: float
    timestamp: Optional[int] = None  # the Signal message's sent timestamp, once it is sent


@dataclass
class Prompts:
    """The prompts waiting for an answer, by code and by the message they were sent in."""

    ttl: float
    clock: Callable[[], float] = time.monotonic
    pick: Callable[[int], int] = secrets.randbelow
    cap: int = 256
    _by_code: dict[str, Prompt] = field(default_factory=dict)

    def reserve(self, session_key: str, request_id: str, chat_id: str, deny_only: bool) -> Prompt:
        """A prompt with a code no other waiting prompt has, to be bound once it is sent."""
        self.prune()
        if len(self._by_code) >= self.cap:
            oldest = min(self._by_code.values(), key=lambda p: p.created)
            del self._by_code[oldest.code]
        for _ in range(100):
            code = f"{self.pick(10000):04d}"
            if code not in self._by_code:
                break
        else:
            raise RuntimeError("no free approval code")
        p = Prompt(session_key, request_id, chat_id, code, deny_only, self.clock())
        self._by_code[code] = p
        return p

    def bind(self, prompt: Prompt, timestamp: int) -> None:
        prompt.timestamp = timestamp

    def drop(self, prompt: Prompt) -> None:
        if self._by_code.get(prompt.code) is prompt:
            del self._by_code[prompt.code]

    def by_code(self, code: str) -> Optional[Prompt]:
        self.prune()
        return self._by_code.get(code)

    def by_timestamp(self, timestamp: int) -> Optional[Prompt]:
        self.prune()
        return next((p for p in self._by_code.values() if p.timestamp == timestamp), None)

    def prune(self) -> None:
        cutoff = self.clock() - self.ttl
        for code in [c for c, p in self._by_code.items() if p.created < cutoff]:
            del self._by_code[code]

    def __len__(self) -> int:
        return len(self._by_code)


@dataclass
class PreApprovals:
    """Commands Jason named in backticks or on a "run:" line in his latest Signal message, by chat (Suite D42: a
    pre-approval comes from Jason directly and covers only the named action). Each covers one
    call whose command is exactly that text. His next message replaces them, and they lapse after
    ttl, so one doesn't outlive the turn it was given for."""

    ttl: float
    clock: Callable[[], float] = time.monotonic
    _by_chat: dict[str, tuple[float, list[str]]] = field(default_factory=dict)

    def record(self, chat_id: str, text: Optional[str]) -> None:
        commands = commands_in(text)
        if commands:
            self._by_chat[chat_id] = (self.clock(), commands)
        else:
            self._by_chat.pop(chat_id, None)

    def take(self, chat_id: str, command: Optional[str]) -> bool:
        """True, using it up, when the chat's latest message named exactly this command."""
        entry = self._by_chat.get(chat_id)
        wanted = (command or "").strip()
        if entry is None or not wanted:
            return False
        at, commands = entry
        if self.clock() - at > self.ttl:
            del self._by_chat[chat_id]
            return False
        if wanted not in commands:
            return False
        commands.remove(wanted)
        if not commands:
            del self._by_chat[chat_id]
        return True
