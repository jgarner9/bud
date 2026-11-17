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
        self.assertIsNone(ps.by_code(p1.code))

    def test_drop_leaves_a_newer_prompt_with_the_same_code(self):
        ps = a.Prompts(ttl=60, pick=lambda n: 5)
        p1 = ps.reserve("s", "r1", "+1", False)
        ps.drop(p1)
        p2 = ps.reserve("s", "r2", "+1", False)
        ps.drop(p1)
        self.assertIs(ps.by_code("0005"), p2)


if __name__ == "__main__":
    unittest.main()
