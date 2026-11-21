"""The approval reviewer socket (Suite D103): bud's own processes (the MCP gateway, ops) ask Jason on
Signal through this plugin. Standard library only; adapter.py supplies the Signal side.

Protocol, newline-delimited JSON over a unix socket (internal/approval/socket.go is the client):
the client sends one line {id, summary, expires_at, secret} and holds the connection open; the plugin
answers one line {id, choice} with choice "once" or "deny", or {id, error} to refuse. The client
closing the connection withdraws the prompt. Every failure denies: nothing here ever approves unless a
sender the allowlist names said so.
"""

from __future__ import annotations

import asyncio
import hmac
import json
import logging
import os
import stat
import time
from datetime import datetime, timezone
from typing import Any, Optional

logger = logging.getLogger(__name__)

MAX_LINE = 64 << 10  # the client's own limit
MAX_WAIT = 3600  # an unanswered approval is denied after an hour (Suite D42)
READ_TIMEOUT = 10  # for the request line to arrive
MAX_SUMMARY = 2000
MAX_SECRET = 4096
FIELDS = frozenset({"id", "summary", "expires_at", "secret"})

SOCKET_ENV = "BUD_REVIEW_SOCKET"
SECRET_ENV = "BUD_REVIEW_SECRET_FILE"


def default_paths(env=None) -> tuple[str, str]:
    """``(socket, secret_file)`` from BUD_REVIEW_SOCKET and BUD_REVIEW_SECRET_FILE, else the defaults the Go
    side uses: $XDG_RUNTIME_DIR/bud/review.sock and $XDG_STATE_HOME/bud/review.secret (state falls back to
    ~/.local/state). The socket is empty when there is no absolute runtime dir: no fallback to /tmp."""
    env = os.environ if env is None else env
    sock = env.get(SOCKET_ENV, "")
    if not sock:
        runtime = env.get("XDG_RUNTIME_DIR", "")
        sock = os.path.join(runtime, "bud", "review.sock") if os.path.isabs(runtime) else ""
    secret = env.get(SECRET_ENV, "")
    if not secret:
        state = env.get("XDG_STATE_HOME", "")
        if not os.path.isabs(state):
            home = env.get("HOME", "")
            state = os.path.join(home, ".local", "state") if os.path.isabs(home) else ""
        secret = os.path.join(state, "bud", "review.secret") if state else ""
    return sock, secret


def read_secret(path: str) -> str:
    """The secret in path; ValueError unless it is a non-empty regular file with mode 0600 and at most
    MAX_SECRET bytes. Like the Go side: no-follow so a symlink is refused, non-blocking so a FIFO can't hang
    the review, and the checks run on the opened file, not the path."""
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    except OSError as e:
        raise ValueError(f"approval secret {path} unreadable (it must be a regular file, not a symlink): {e.strerror}") from None
    try:
        st = os.fstat(fd)
        if not stat.S_ISREG(st.st_mode) or st.st_mode & 0o077:
            raise ValueError(f"approval secret {path} must be a regular file with mode 0600")
        data = os.read(fd, MAX_SECRET + 1)
    except OSError as e:
        raise ValueError(f"approval secret unreadable: {e.strerror}") from None
    finally:
        os.close(fd)
    if len(data) > MAX_SECRET:
        raise ValueError(f"approval secret {path} is over {MAX_SECRET} bytes")
    try:
        value = data.decode("utf-8").strip()
    except UnicodeDecodeError:
        raise ValueError("approval secret is not UTF-8") from None
    if not value:
        raise ValueError("approval secret is empty")
    return value


def parse_expiry(value: Any) -> Optional[float]:
    """Unix time of an RFC 3339 UTC "YYYY-MM-DDTHH:MM:SSZ", or None when it isn't one."""
    if not isinstance(value, str):
        return None
    try:
        return datetime.strptime(value, "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=timezone.utc).timestamp()
    except ValueError:
        return None


class ReviewServer:
    """Serves reviews on a unix socket. host supplies the Signal side:

    ``await host.open_review(summary)`` posts the prompt and returns a handle with ``.review``, a future (set to
    "once" or "deny" by whoever answers); ``await host.close_review(handle, note)`` forgets it so a later
    answer does nothing, and says note on Signal when given."""

    def __init__(self, socket_path: str, secret_file: str, host: Any, clock=time.time):
        self.socket_path, self.secret_file, self.host, self.clock = socket_path, secret_file, host, clock
        self._server: Optional[asyncio.AbstractServer] = None
        self._active: dict = {}  # handler task -> its open prompt handle, or None before one is posted

    def check(self) -> None:
        """Raise ValueError when the listener must not start."""
        if not self.socket_path or not self.secret_file:
            raise ValueError("no runtime directory or secret file configured")
        read_secret(self.secret_file)
        parent = os.path.dirname(self.socket_path)
        try:
            st = os.stat(parent)
        except FileNotFoundError:
            return  # start creates it 0700
        except OSError as e:
            raise ValueError(f"socket directory unreadable: {e.strerror}") from None
        if not stat.S_ISDIR(st.st_mode) or st.st_mode & 0o077 or st.st_uid != os.geteuid():
            raise ValueError(f"socket directory {parent} must be a directory of ours with mode 0700")

    async def start(self) -> bool:
        """Listen; False, with the reason logged, when it must not. Never raises, so Hermes carries on."""
        try:
            self.check()
            parent = os.path.dirname(self.socket_path)
            os.makedirs(parent, mode=0o700, exist_ok=True)
            self.check()
            if os.path.lexists(self.socket_path):
                if not stat.S_ISSOCK(os.lstat(self.socket_path).st_mode):
                    raise ValueError(f"{self.socket_path} exists and is not a socket")
                os.unlink(self.socket_path)
            self._server = await asyncio.start_unix_server(self._serve, path=self.socket_path, limit=MAX_LINE)
            os.chmod(self.socket_path, 0o600)
        except (ValueError, OSError) as e:
            logger.warning("bud-signal: approval reviews are off: %s", e)
            await self.stop()
            return False
        logger.info("bud-signal: serving approval reviews on %s", self.socket_path)
        return True

    async def stop(self) -> None:
        server, self._server = self._server, None
        if server is not None:
            server.close()
            try:
                os.unlink(self.socket_path)
            except OSError:
                pass
        # server.close() leaves in-flight handlers waiting: withdraw their prompts so a later reaction does
        # nothing, then cancel them (the finally in _serve closes each connection)
        tasks = list(self._active)
        for task in tasks:
            handle = self._active.get(task)
            if handle is not None:
                self._active[task] = None
                try:
                    await self.host.close_review(handle, "That approval was cancelled: the reviewer is stopping.")
                except Exception:
                    logger.exception("bud-signal: couldn't withdraw an approval review")
            task.cancel()
        if tasks:
            await asyncio.gather(*tasks, return_exceptions=True)
        if server is not None:
            await server.wait_closed()

    async def _serve(self, reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
        task = asyncio.current_task()
        self._active[task] = None
        try:
            await self._review(reader, writer, task)
        except asyncio.CancelledError:
            await self._say(writer, {"id": "", "choice": "deny"})
        except Exception:
            logger.exception("bud-signal: approval review failed")
            await self._say(writer, {"id": "", "error": "internal error"})
        finally:
            self._active.pop(task, None)
            writer.close()

    @staticmethod
    async def _say(writer: asyncio.StreamWriter, obj: dict) -> None:
        try:
            writer.write(json.dumps(obj).encode() + b"\n")
            await writer.drain()
        except (ConnectionError, OSError):
            pass

    async def _review(self, reader: asyncio.StreamReader, writer: asyncio.StreamWriter, task=None) -> None:
        try:
            raw = await asyncio.wait_for(reader.readline(), READ_TIMEOUT)
        except (ValueError, asyncio.LimitOverrunError):
            return await self._say(writer, {"id": "", "error": "request too large"})
        except asyncio.TimeoutError:
            return await self._say(writer, {"id": "", "error": "no request"})
        if not raw.endswith(b"\n"):
            return  # closed before a whole line
        try:
            req = json.loads(raw)
        except ValueError:
            req = None
        if not isinstance(req, dict):
            return await self._say(writer, {"id": "", "error": "malformed request"})
        if set(req) != FIELDS:
            return await self._say(writer, {"id": "", "error": "malformed request: unexpected fields"})
        rid = req.get("id")
        if not isinstance(rid, str) or not rid or len(rid) > 128:
            return await self._say(writer, {"id": "", "error": "malformed request"})

        def refuse(why: str):
            return self._say(writer, {"id": rid, "error": why})

        try:
            secret = read_secret(self.secret_file)
        except ValueError as e:
            logger.warning("bud-signal: %s", e)
            return await refuse("reviewer secret unavailable")
        given = req.get("secret")
        # compare bytes: compare_digest on str raises for non-ASCII
        if not isinstance(given, str) or not hmac.compare_digest(given.encode(), secret.encode()):
            return await refuse("bad secret")
        summary = req.get("summary")
        expires = parse_expiry(req.get("expires_at"))
        if not isinstance(summary, str) or not summary.strip() or expires is None:
            return await refuse("malformed request")
        wait = min(expires - self.clock(), MAX_WAIT)
        if wait <= 0:
            return await self._say(writer, {"id": rid, "choice": "deny"})

        try:
            handle = await self.host.open_review(summary[:MAX_SUMMARY])
        except Exception as e:
            logger.warning("bud-signal: couldn't post an approval review: %s", e)
            return await refuse("couldn't send the prompt")
        if task is not None:
            self._active[task] = handle
        # the send may have been slow: the wait is what is left of the expiry now
        wait = min(expires - self.clock(), MAX_WAIT)
        if wait <= 0:
            await self._say(writer, {"id": rid, "choice": "deny"})
            return await self.host.close_review(handle, "That approval timed out and was denied.")

        # EOF (or any byte) from the client means it has given up
        gone = asyncio.ensure_future(reader.read(1))
        try:
            done, _ = await asyncio.wait({handle.review, gone}, timeout=wait, return_when=asyncio.FIRST_COMPLETED)
            if handle.review in done:
                choice = handle.review.result()
                await self._say(writer, {"id": rid, "choice": "once" if choice == "once" else "deny"})
                await self.host.close_review(handle, None)
            elif gone in done:
                await self.host.close_review(handle, "That approval was withdrawn by the requester.")
            else:
                await self._say(writer, {"id": rid, "choice": "deny"})
                await self.host.close_review(handle, "That approval timed out and was denied.")
        finally:
            gone.cancel()
