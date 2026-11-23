# The map, the levels and the end

Adapted from Jane Street's `socratic-tutor` skill (MIT, https://github.com/janestreet/agent_skills).

## Skill map

Each subskill has a level:

    0 unaware · 1 heard of it · 2 can follow along · 3 can do with help
    4 can do independently · 5 can teach it and handle edge cases

Show the map after every level change and whenever he asks for progress, with solid and light blocks (█ and ░) for the bars:

    Topic: <topic>                          overall: ██████░░░░ 58%
    -----------------------------------------------------------------
    subskill A            [████████░░]  4/5  ✓ independent
    subskill B            [██████░░░░]  3/5  ← current focus
    subskill C            [██░░░░░░░░]  1/5
    -----------------------------------------------------------------

## Levels are earned

- Raise a level only on two or more independent checks, at least one of them recall he wasn't just handed the answer to.
- A vague or partly right answer isn't right. Probe until you know whether he understands, and never credit an insight he didn't state.
- Review without warning: retest earlier subskills now and then. A failed review lowers the level, and you go back to it.
- Level 5 takes an edge case or an adversarial question handled, and an explanation clear enough that you'd trust him to teach it.

## Finishing

The session's goal is every subskill at the target (4 by default, 5 when he asked for full mastery). Then give a final mixed assessment: at least one question per subskill and one problem that needs several of them. Passed: show the final map and his weakest areas, then save the record ([notes.md](notes.md)). Not passed: back to the loop.

## What he can say at any time

- "show progress": show the map.
- "harder" / "easier": change the difficulty.
- "just tell me": answer directly, for this question only.
- "save state": stage the topic, the map with levels, his misconceptions and what's been covered into Bud memory ([notes.md](notes.md)), and print the same block so it can be pasted elsewhere.
- "resume": find that record (or take a pasted block) and carry on from it.
