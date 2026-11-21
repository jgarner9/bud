"""Tests for bud-signal's approvals module. Run: python3 -m unittest discover -s server/hermes/plugins/tests"""

import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "bud-signal"))

import approvals as a  # noqa: E402


def dm(sender="+15550001111", **message):
    return {"envelope": {"sourceNumber": sender, "sourceUuid": "uuid-j", "timestamp": 5, "dataMessage": message}}


class ReactionChoice(unittest.TestCase):
    def test_thumbs_and_their_skin_tones(self):
        self.assertEqual(a.reaction_choice("\U0001F44D"), a.APPROVE)
        self.assertEqual(a.reaction_choice("\U0001F44D\U0001F3FD"), a.APPROVE)
        self.assertEqual(a.reaction_choice("\U0001F44E️"), a.DENY)

    def test_any_other_emoji_is_no_answer(self):
        for e in ["❤️", "\U0001F44C", "", None, "\U0001F44D\U0001F44D"]:
            self.assertIsNone(a.reaction_choice(e), e)


class ParseCode(unittest.TestCase):
    def test_yes_and_no_with_a_code(self):
        self.assertEqual(a.parse_code("yes 4821"), (a.APPROVE, "4821"))
        self.assertEqual(a.parse_code("  No 0042. "), (a.DENY, "0042"))
        self.assertEqual(a.parse_code("YES 1234!"), (a.APPROVE, "1234"))

    def test_anything_else_is_a_message(self):
        for t in ["yes", "yes 123", "yes 12345", "yes 1234 please", "ok 1234", "sure, yes 1234", "", None]:
            self.assertIsNone(a.parse_code(t), t)


class Footer(unittest.TestCase):
    def test_names_the_code_and_both_answers(self):
        f = a.footer("4821", False)
        for part in ["yes 4821", "no 4821", a.THUMBS_UP, a.THUMBS_DOWN, "once"]:
            self.assertIn(part, f)

    def test_deny_only_offers_no_approval(self):
        f = a.footer("4821", True)
        self.assertIn("no 4821", f)
        self.assertNotIn("yes 4821", f)
        self.assertNotIn(a.THUMBS_UP, f)


class Envelopes(unittest.TestCase):
    def test_a_reaction(self):
        r = a.reaction_of(dm(reaction={"emoji": "\U0001F44D", "targetAuthorNumber": "+15559990000",
                                       "targetAuthorUuid": "uuid-bot", "targetSentTimestamp": 1700, "isRemove": False}))
        self.assertEqual(r, a.Reaction("+15550001111", "\U0001F44D", "+15559990000", "uuid-bot", 1700, False))

    def test_a_removed_reaction_says_so(self):
        r = a.reaction_of(dm(reaction={"emoji": "\U0001F44D", "targetSentTimestamp": 1700, "isRemove": True}))
        self.assertTrue(r.remove)

    def test_groups_sync_messages_and_junk_are_never_answers(self):
        group = dm(reaction={"emoji": "\U0001F44D", "targetSentTimestamp": 1700}, groupInfo={"groupId": "g"})
        sync = {"envelope": {"sourceNumber": "+1", "syncMessage": {"sentMessage": {"reaction": {}}}}}
        for e in [group, sync, dm(reaction={"emoji": "x", "targetSentTimestamp": "nope"}),
                  dm(reaction={"emoji": "x"}), dm(message="hi"), {"envelope": {"dataMessage": {"reaction": {}}}}, {}, []]:
            self.assertIsNone(a.reaction_of(e), e)
        self.assertIsNone(a.text_of(dm(message="yes 1234", groupInfo={"groupId": "g"})))
        self.assertIsNone(a.text_of(sync))

    def test_a_text(self):
        self.assertEqual(a.text_of(dm(message="yes 1234")), a.Text("+15550001111", "yes 1234"))
        self.assertIsNone(a.text_of(dm(message=None)))

    def test_the_uuid_stands_in_for_a_missing_number(self):
        e = {"sourceUuid": "uuid-j", "dataMessage": {"message": "no 1111"}}
        self.assertEqual(a.text_of(e), a.Text("uuid-j", "no 1111"))


class MayAnswer(unittest.TestCase):
    def test_only_a_named_sender(self):
        self.assertTrue(a.may_answer("+1", {"+1"}))
        self.assertFalse(a.may_answer("+2", {"+1"}))

    def test_an_open_or_empty_list_names_nobody(self):
        self.assertFalse(a.may_answer("+1", {"*"}))
        self.assertFalse(a.may_answer("+1", {"*", "+1"}))
        self.assertFalse(a.may_answer("+1", set()))
        self.assertFalse(a.may_answer("", {""}))


class Clock:
    def __init__(self):
        self.now = 1000.0

    def __call__(self):
        return self.now


class PromptsTest(unittest.TestCase):
    def test_codes_are_unique_among_waiting_prompts(self):
        picks = iter([7, 7, 7, 12])
        ps = a.Prompts(ttl=60, pick=lambda n: next(picks))
        p1 = ps.reserve("s", "r1", "+1", False)
        p2 = ps.reserve("s", "r2", "+1", False)
        self.assertEqual((p1.code, p2.code), ("0007", "0012"))

    def test_found_by_code_and_by_the_message_once_bound(self):
        ps = a.Prompts(ttl=60)
        p = ps.reserve("s", "r1", "+1", False)
        self.assertIsNone(ps.by_timestamp(1700))
        ps.bind(p, 1700)
        self.assertIs(ps.by_timestamp(1700), p)
        self.assertIs(ps.by_code(p.code), p)
        ps.drop(p)
        self.assertIsNone(ps.by_code(p.code))
        self.assertEqual(len(ps), 0)

    def test_prompts_expire(self):
        clock = Clock()
        ps = a.Prompts(ttl=60, clock=clock)
        p = ps.reserve("s", "r1", "+1", False)
        clock.now += 59
        self.assertIs(ps.by_code(p.code), p)
        clock.now += 2
        self.assertIsNone(ps.by_code(p.code))

    def test_the_oldest_goes_when_full(self):
        clock = Clock()
        ps = a.Prompts(ttl=600, clock=clock, cap=2)
        p1 = ps.reserve("s", "r1", "+1", False)
        clock.now += 1
        ps.reserve("s", "r2", "+1", False)
        clock.now += 1
        ps.reserve("s", "r3", "+1", False)
        self.assertEqual(len(ps), 2)
        # r1's code is free once it's gone, so r3 may draw it: what matters
        # is that the code no longer finds r1
        found = ps.by_code(p1.code)
        self.assertTrue(found is None or found.request_id != "r1", found)

    def test_drop_leaves_a_newer_prompt_with_the_same_code(self):
        ps = a.Prompts(ttl=60, pick=lambda n: 5)
        p1 = ps.reserve("s", "r1", "+1", False)
        ps.drop(p1)
        p2 = ps.reserve("s", "r2", "+1", False)
        ps.drop(p1)
        self.assertIs(ps.by_code("0005"), p2)



class CommandsIn(unittest.TestCase):
    def test_backticked_commands_only(self):
        self.assertEqual(a.commands_in("please run `git push` then ` make test `"), ["git push", "make test"])
        self.assertEqual(a.commands_in("run this:\n```sh\ngo test ./...\n```"), ["go test ./..."])
        self.assertEqual(a.commands_in("push the branch for me"), [])
        self.assertEqual(a.commands_in("an empty `` pair"), [])
        self.assertEqual(a.commands_in(None), [])

    def test_run_lines(self):
        self.assertEqual(a.commands_in("run: touch /tmp/bud-approval-test"), ["touch /tmp/bud-approval-test"])
        self.assertEqual(a.commands_in("ok\n  Run:  git push  \nthanks"), ["git push"])
        self.assertEqual(a.commands_in("run: make\nrun: make test"), ["make", "make test"])
        # only at the start of a line, and not an empty one
        self.assertEqual(a.commands_in("please run: git push"), [])
        self.assertEqual(a.commands_in("run:"), [])
        self.assertEqual(a.commands_in("rerun: git push"), [])
        # inside a fenced block it is part of that block's command
        self.assertEqual(a.commands_in("```\nrun: rm -rf x\n```"), ["run: rm -rf x"])

    def test_a_run_line_typed_inside_backticks(self):
        self.assertEqual(a.commands_in("`run: touch x`"), ["touch x"])
        self.assertEqual(a.commands_in("please ` RUN:  git push ` now"), ["git push"])
        self.assertEqual(a.commands_in("`run:`"), [])
        # only a leading one
        self.assertEqual(a.commands_in("`echo run: x`"), ["echo run: x"])


# reasons as bud's gate (internal/adapter/hermes/gate.go askCommand) writes them, after Hermes decodes the hook's JSON
GATE_REASON = 'bud: run "touch /tmp/bud-approval-test"? It needs your approval (Suite D10).'
GATE_TRICKY = 'bud: run "echo \\"\\"? It needs\\" && git push"? It needs your approval (Suite D10).'


class CommandOf(unittest.TestCase):
    def test_a_gate_prompt_names_the_command_in_its_reason(self):
        self.assertEqual(a.command_of(a.TERMINAL_RULE_LABEL, GATE_REASON), "touch /tmp/bud-approval-test")
        self.assertEqual(a.command_of(a.TERMINAL_RULE_LABEL,
                                      'bud: run "go test ./... && git push"? Its part "git push" needs your approval '
                                      '(Suite D10).'), "go test ./... && git push")

    def test_quotes_in_the_command_cant_end_it_early(self):
        self.assertEqual(a.command_of(a.TERMINAL_RULE_LABEL, GATE_TRICKY), 'echo ""? It needs" && git push')

    def test_hermess_own_prompt_carries_the_command(self):
        self.assertEqual(a.command_of("rm -rf build", "recursive delete"), "rm -rf build")

    def test_anything_else_names_nothing(self):
        for command, description in [
            (a.TERMINAL_RULE_LABEL, None),
            (a.TERMINAL_RULE_LABEL, ""),
            (a.TERMINAL_RULE_LABEL, "touch /tmp/bud-approval-test"),
            # the shape must start the description, so text quoted later in it counts for nothing
            (a.TERMINAL_RULE_LABEL, "Plugin requires approval: " + GATE_REASON),
            (a.TERMINAL_RULE_LABEL, " " + GATE_REASON),
            (a.TERMINAL_RULE_LABEL, 'bud: run touch x? It needs your approval (Suite D10).'),
            (a.TERMINAL_RULE_LABEL, 'bud: run "touch x'),
            (a.TERMINAL_RULE_LABEL, 'bud: run "touch x"'),
            (a.TERMINAL_RULE_LABEL, 'bud: run "touch x". It needs your approval.'),
            (a.TERMINAL_RULE_LABEL, 'bud: run 7? It needs your approval.'),
            # another plugin rule, another tool's, never names a command
            ("<mcp__linear__save_issue> (plugin approval rule)", GATE_REASON),
        ]:
            with self.subTest(command=command, description=description):
                self.assertIsNone(a.command_of(command, description))


class PreApprovalsTest(unittest.TestCase):
    def setUp(self):
        self.now = 100.0
        self.pa = a.PreApprovals(ttl=60, clock=lambda: self.now)

    def test_it_covers_the_exact_command_once(self):
        self.pa.record("j", "run `git push`")
        self.assertFalse(self.pa.take("j", "git push --force"))
        self.assertTrue(self.pa.take("j", " git push "))
        self.assertFalse(self.pa.take("j", "git push"))

    def test_a_run_line_covers_the_exact_command_once(self):
        self.pa.record("j", "run: touch /tmp/bud-approval-test")
        self.assertFalse(self.pa.take("j", "touch /tmp/bud-approval-test2"))
        self.assertFalse(self.pa.take("j", "touch  /tmp/bud-approval-test"))
        self.assertTrue(self.pa.take("j", "touch /tmp/bud-approval-test"))
        self.assertFalse(self.pa.take("j", "touch /tmp/bud-approval-test"))

    def test_only_that_chat(self):
        self.pa.record("j", "run `git push`")
        self.assertFalse(self.pa.take("x", "git push"))

    def test_the_next_message_replaces_it(self):
        self.pa.record("j", "run `git push`")
        self.pa.record("j", "thanks")
        self.assertFalse(self.pa.take("j", "git push"))

    def test_it_lapses(self):
        self.pa.record("j", "run `git push`")
        self.now += 61
        self.assertFalse(self.pa.take("j", "git push"))

    def test_an_empty_command_never_matches(self):
        self.pa.record("j", "run `git push`")
        self.assertFalse(self.pa.take("j", ""))
        self.assertFalse(self.pa.take("j", None))


if __name__ == "__main__":
    unittest.main()
