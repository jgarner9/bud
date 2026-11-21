"""Tests for bud-signal's reviewer socket, over a real unix socket in a temp dir, against the same
Hermes stand-ins as test_adapter.py."""

import asyncio
import json
import os
import shutil
import tempfile
import time
import unittest

from test_adapter import BOTH, BOT, JASON, OTHER, UP, DOWN, Approvals, Config, ExecApprovalPrompt, load, reaction, text

SECRET = "s3cret-value"


def iso(offset=3600):
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time() + offset))


class ReviewerTest(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.approvals = Approvals()
        registered = load(self.approvals)
        self.a = registered["adapter_factory"](Config([JASON]))
        self.dir = tempfile.mkdtemp(prefix="bsr")
        self.addCleanup(shutil.rmtree, self.dir, True)
        os.chmod(self.dir, 0o700)
        self.sock = os.path.join(self.dir, "run", "review.sock")
        self.secret = os.path.join(self.dir, "review.secret")
        with open(self.secret, "w") as f:
            f.write(SECRET + "\n")
        os.chmod(self.secret, 0o600)
        self.reviewer_mod = __import__("sys").modules["bud_signal.reviewer"]
        self.now = time.time()
        self.server = self.reviewer_mod.ReviewServer(self.sock, self.secret, self.a, clock=lambda: self.now)
        self.assertTrue(await self.server.start())
        self.addAsyncCleanup(self.server.stop)

    async def connect(self):
        r, w = await asyncio.open_unix_connection(self.sock)
        self.addCleanup(w.close)
        return r, w

    async def ask(self, rid="a1", secret=SECRET, expires=None):
        r, w = await self.connect()
        w.write(json.dumps({"id": rid, "summary": "linear save_issue", "expires_at": expires or iso(),
                            "secret": secret}).encode() + b"\n")
        await w.drain()
        return r, w

    async def prompt(self):
        for _ in range(200):
            if len(self.a._prompts):
                return next(iter(self.a._prompts._by_code.values()))
            await asyncio.sleep(0.01)
        self.fail("no prompt was sent")

    async def reply(self, r):
        return json.loads(await asyncio.wait_for(r.readline(), 5))

    async def test_socket_and_dir_are_private(self):
        self.assertEqual(os.stat(self.sock).st_mode & 0o777, 0o600)
        self.assertEqual(os.stat(os.path.dirname(self.sock)).st_mode & 0o777, 0o700)

    async def test_a_thumbs_up_allows_once(self):
        r, _ = await self.ask()
        p = await self.prompt()
        self.assertIn("linear save_issue", self.a.sent[-1][1])
        await self.a._handle_envelope(reaction(UP, p.timestamp))
        self.assertEqual(await self.reply(r), {"id": "a1", "choice": "once"})
        self.assertEqual(self.a.reactions, [(JASON, "✅", BOT, p.timestamp)])
        self.assertEqual(self.a.passed, [])

    async def test_yes_and_the_code_allows_once(self):
        r, _ = await self.ask()
        p = await self.prompt()
        await self.a._handle_envelope(text(f"yes {p.code}"))
        self.assertEqual(await self.reply(r), {"id": "a1", "choice": "once"})

    async def test_a_thumbs_down_or_no_denies(self):
        r, _ = await self.ask()
        p = await self.prompt()
        await self.a._handle_envelope(reaction(DOWN, p.timestamp))
        self.assertEqual(await self.reply(r), {"id": "a1", "choice": "deny"})
        r, _ = await self.ask("a2")
        p = await self.prompt()
        await self.a._handle_envelope(text(f"no {p.code}"))
        self.assertEqual(await self.reply(r), {"id": "a2", "choice": "deny"})

    async def test_a_bad_secret_is_refused_and_nothing_is_sent(self):
        r, _ = await self.ask(secret="wrong")
        self.assertEqual(await self.reply(r), {"id": "a1", "error": "bad secret"})
        self.assertEqual(self.a.sent, [])

    async def test_an_oversized_line_is_refused(self):
        r, w = await self.connect()
        w.write(b"x" * (70 << 10))
        try:
            await w.drain()
        except ConnectionError:
            pass
        self.assertIn("error", await self.reply(r))
        self.assertEqual(self.a.sent, [])

    async def test_the_client_closing_withdraws_the_prompt(self):
        r, w = await self.ask()
        p = await self.prompt()
        w.close()
        for _ in range(200):
            if len(self.a._prompts) == 0:
                break
            await asyncio.sleep(0.01)
        self.assertEqual(len(self.a._prompts), 0)
        self.assertIn("withdrawn", self.a.sent[-1][1])
        await self.a._handle_envelope(reaction(UP, p.timestamp))
        self.assertEqual(self.approvals.calls, [])
        self.assertEqual(len(self.a.reactions), 0)

    async def test_it_denies_when_the_time_is_up(self):
        # the wait is expires_at minus the server's clock, so a clock just short of it times out at once
        expires = iso(3600)
        at = self.reviewer_mod.parse_expiry(expires)
        self.server.clock = lambda: at - 0.2
        r, _ = await self.ask(expires=expires)
        self.assertEqual(await self.reply(r), {"id": "a1", "choice": "deny"})
        self.assertEqual(len(self.a._prompts), 0)
        self.assertIn("timed out", self.a.sent[-1][1])

    async def test_an_expiry_already_past_denies_without_asking(self):
        r, _ = await self.ask(expires=iso(-5))
        self.assertEqual(await self.reply(r), {"id": "a1", "choice": "deny"})
        self.assertEqual(self.a.sent, [])

    async def test_the_wait_is_capped_at_an_hour(self):
        waits = []
        real = asyncio.wait

        async def spy(fs, timeout=None, return_when=None):
            waits.append(timeout)
            return await real(fs, timeout=0.05, return_when=return_when)
        asyncio.wait = spy
        self.addCleanup(setattr, asyncio, "wait", real)
        r, _ = await self.ask(expires=iso(7200))
        self.assertEqual(await self.reply(r), {"id": "a1", "choice": "deny"})
        self.assertLessEqual(waits[0], 3600)

    async def test_expiry_passing_during_a_slow_send_denies_and_withdraws(self):
        expires = iso(3600)
        at = self.reviewer_mod.parse_expiry(expires)
        self.server.clock = lambda: at - 100
        real = self.a.open_review

        async def slow(summary):
            handle = await real(summary)
            self.server.clock = lambda: at + 1  # the send took until after the expiry
            return handle
        self.a.open_review = slow
        r, _ = await self.ask(expires=expires)
        self.assertEqual(await self.reply(r), {"id": "a1", "choice": "deny"})
        self.assertEqual(len(self.a._prompts), 0)
        self.assertIn("timed out", self.a.sent[-1][1])

    async def test_stop_withdraws_an_in_flight_review_and_a_later_reaction_is_ignored(self):
        r, _ = await self.ask()
        p = await self.prompt()
        await self.server.stop()
        self.assertEqual(len(self.a._prompts), 0)
        self.assertEqual(self.server._active, {})
        await self.a._handle_envelope(reaction(UP, p.timestamp))
        self.assertEqual(self.a.reactions, [])
        self.assertEqual(self.approvals.calls, [])
        self.assertNotEqual((await self.reply(r)).get("choice"), "once")

    async def test_an_extra_key_is_refused_and_nothing_is_sent(self):
        r, w = await self.connect()
        w.write(json.dumps({"id": "a1", "summary": "s", "expires_at": iso(), "secret": SECRET,
                            "extra": 1}).encode() + b"\n")
        await w.drain()
        self.assertIn("error", await self.reply(r))
        self.assertEqual(self.a.sent, [])

    async def test_a_missing_key_is_refused_too(self):
        r, w = await self.connect()
        w.write(json.dumps({"id": "a1", "summary": "s", "secret": SECRET}).encode() + b"\n")
        await w.drain()
        self.assertIn("error", await self.reply(r))
        self.assertEqual(self.a.sent, [])

    async def test_an_unsafe_secret_file_is_refused_at_request_time(self):
        os.unlink(self.secret)
        target = os.path.join(self.dir, "real")
        with open(target, "w") as f:
            f.write(SECRET)
        os.chmod(target, 0o600)
        os.symlink(target, self.secret)
        r, _ = await self.ask()
        self.assertIn("error", await self.reply(r))
        os.unlink(self.secret)
        os.mkfifo(self.secret, 0o600)  # must not block
        r, _ = await self.ask("a2")
        self.assertIn("error", await self.reply(r))
        os.unlink(self.secret)
        with open(self.secret, "w") as f:
            f.write("x" * 5000)
        os.chmod(self.secret, 0o600)
        r, _ = await self.ask("a3")
        self.assertIn("error", await self.reply(r))
        self.assertEqual(self.a.sent, [])

    async def test_a_sender_who_is_not_allowed_answers_nothing(self):
        r, w = await self.ask()
        p = await self.prompt()
        await self.a._handle_envelope(reaction(UP, p.timestamp, sender=OTHER))
        await self.a._handle_envelope(text(f"yes {p.code}", sender="+15554445555"))
        self.assertFalse(p.review.done())
        w.close()

    async def test_a_reviewer_code_never_collides_with_a_hermes_approval(self):
        codes = set()
        for i in range(30):
            self.approvals.pending[f"r{i}"] = ("sess", JASON, False)
            await self.a._send_exec_approval_prompt(ExecApprovalPrompt(
                chat_id=JASON, session_key="sess", text="t", actions=BOTH, request_id=f"r{i}"))
        for i in range(30):
            p = await self.a.open_review("x")
            codes.add(p.code)
        self.assertEqual(len(self.a._prompts), 60)
        self.assertEqual(len({p.code for p in self.a._prompts._by_code.values()}), 60)

    async def test_a_hermes_code_does_not_answer_a_review_and_the_reverse(self):
        self.approvals.pending["r1"] = ("sess", JASON, False)
        await self.a._send_exec_approval_prompt(ExecApprovalPrompt(
            chat_id=JASON, session_key="sess", text="t", actions=BOTH, request_id="r1"))
        r, _ = await self.ask()
        await asyncio.sleep(0.05)
        review = next(p for p in self.a._prompts._by_code.values() if p.review is not None)
        await self.a._handle_envelope(reaction(UP, review.timestamp))
        self.assertEqual(self.approvals.calls, [])
        self.assertEqual(await self.reply(r), {"id": "a1", "choice": "once"})


class StartTest(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        self.mod = None
        self.dir = tempfile.mkdtemp(prefix="bsr")
        self.addCleanup(shutil.rmtree, self.dir, True)
        self.a = load(Approvals())["adapter_factory"](Config([JASON]))
        self.mod = __import__("sys").modules["bud_signal.reviewer"]

    async def start(self, run_mode, secret_mode=0o600, write_secret=True):
        run = os.path.join(self.dir, "run")
        os.mkdir(run)
        os.chmod(run, run_mode)
        secret = os.path.join(self.dir, "secret")
        if write_secret:
            with open(secret, "w") as f:
                f.write("x")
            os.chmod(secret, secret_mode)
        s = self.mod.ReviewServer(os.path.join(run, "r.sock"), secret, self.a)
        self.addAsyncCleanup(s.stop)
        return await s.start()

    async def test_it_starts_with_private_paths(self):
        self.assertTrue(await self.start(0o700))

    async def test_it_refuses_a_group_accessible_directory(self):
        self.assertFalse(await self.start(0o750))

    async def test_it_refuses_a_loose_or_missing_secret(self):
        self.assertFalse(await self.start(0o700, secret_mode=0o640))

    async def test_it_refuses_a_missing_secret(self):
        self.assertFalse(await self.start(0o700, write_secret=False))

    async def test_it_refuses_a_symlinked_secret(self):
        run = os.path.join(self.dir, "run")
        os.mkdir(run, 0o700)
        real = os.path.join(self.dir, "real")
        with open(real, "w") as f:
            f.write("x")
        os.chmod(real, 0o600)
        link = os.path.join(self.dir, "link")
        os.symlink(real, link)
        s = self.mod.ReviewServer(os.path.join(run, "r.sock"), link, self.a)
        self.addAsyncCleanup(s.stop)
        self.assertFalse(await s.start())

    def test_default_paths_follow_the_go_side(self):
        env = {"XDG_RUNTIME_DIR": "/run/user/1", "XDG_STATE_HOME": "/st"}
        self.assertEqual(self.mod.default_paths(env), ("/run/user/1/bud/review.sock", "/st/bud/review.secret"))
        self.assertEqual(self.mod.default_paths({"HOME": "/h"}), ("", "/h/.local/state/bud/review.secret"))
        self.assertEqual(self.mod.default_paths({"BUD_REVIEW_SOCKET": "/a", "BUD_REVIEW_SECRET_FILE": "/b"}),
                         ("/a", "/b"))


if __name__ == "__main__":
    unittest.main()
