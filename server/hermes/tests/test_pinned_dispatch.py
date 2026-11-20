"""Regression probes for the *unmodified* Hermes v0.23.2 dispatch paths.

Run from a trusted, disposable pinned checkout and editable environment, e.g.:
  env -i PATH=/usr/bin:/bin PYTHONNOUSERSITE=1 PYTHONDONTWRITEBYTECODE=1 \
    HERMES_SOURCE=/path/to/hermes /path/to/hermes/.venv/bin/python \
    -m unittest discover -s server/hermes/tests

The tracked-file check does not detect untracked Python modules that could shadow
upstream imports. Do not use an untrusted checkout or virtual environment. Without
HERMES_SOURCE the probes are skipped, not passed.

These tests demonstrate why the existing pre-tool approval hook is insufficient;
they are not acceptance tests for the proposed final-dispatch gate. No real tool,
network request, reviewer, or provider is used.
"""
import os
import subprocess
import sys
import tempfile
import unittest
from types import SimpleNamespace
from unittest.mock import patch

SOURCE = os.environ.get("HERMES_SOURCE")
PIN = "ae2a81ccc00e9b7c06301523e5bb09608397149c"


@unittest.skipUnless(SOURCE, "set HERMES_SOURCE to the Hermes v0.23.2 checkout")
class PinnedDispatchProbes(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        actual = subprocess.check_output(["git", "-C", SOURCE, "rev-parse", "HEAD"], text=True).strip()
        if actual != PIN:
            raise RuntimeError(f"Hermes source is {actual}; expected {PIN}")
        if subprocess.call(["git", "-C", SOURCE, "diff", "--quiet", "HEAD", "--"]) != 0:
            raise RuntimeError("Hermes checkout has tracked modifications; these probes require stock source")
        cls.home = tempfile.TemporaryDirectory(prefix="bud-hermes-probe-")
        cls.environment = patch.dict(os.environ, {"HOME": cls.home.name, "HERMES_HOME": cls.home.name,
                                                  "PATH": os.environ.get("PATH", "")}, clear=True)
        cls.environment.start()
        sys.path.insert(0, SOURCE)

    @classmethod
    def tearDownClass(cls):
        sys.path.remove(SOURCE)
        cls.environment.stop()
        cls.home.cleanup()

    def test_agent_hook_observes_input_before_its_own_rewrite(self):
        from agent import tool_executor as te
        from hermes_cli import plugins

        seen, executed = [], []
        agent = SimpleNamespace(
            _tool_guardrails=SimpleNamespace(before_call=lambda *_: SimpleNamespace(allows_execution=True)),
            tool_progress_callback=None,
            clarify_callback=None,
        )
        state = te._ManagedToolResult(None, {}, [], False, False)
        ref = te._ToolCallRef("synthetic", {"value": "before"}, "task", "call", [])

        def hook(_name, args, **_kwargs):
            seen.append(dict(args))
            return None, {"value": "after"}

        with patch.object(plugins, "_dispatch_pre_tool_call_hooks", hook), \
             patch.object(te, "_begin_tool_execution"), \
             patch.object(te, "_run_with_activity_heartbeat", side_effect=lambda _a, _n, fn: fn()):
            result = te._dispatch_authorized_once(
                agent, state, ref, execute=lambda args: executed.append(dict(args)) or "ok",
                scope_block=None, display_index=None, begin_execution=None, authorization_gate=None,
            )
        self.assertEqual(result, "ok")
        self.assertEqual(seen, [{"value": "before"}])
        self.assertEqual(executed, [{"value": "after"}])
        self.assertEqual(state.args, {"value": "after"})

    def test_direct_registry_path_can_rewrite_after_pre_hook(self):
        import model_tools
        from hermes_cli import middleware, plugins
        from agent import tool_executor
        from tools.code_execution_rpc import _default_dispatch

        seen, executed = [], []

        def hook(_name, args, **_kwargs):
            seen.append(dict(args))
            return None, None

        def rewrite(_name, _args, dispatch, **_kwargs):
            return dispatch({"value": "after"})

        with patch.object(plugins, "_dispatch_pre_tool_call_hooks", hook), \
             patch.object(middleware, "apply_tool_request_middleware", side_effect=lambda _n, a, **_k: SimpleNamespace(payload=a, original_payload=dict(a), trace=[])), \
             patch.object(middleware, "run_tool_execution_middleware", rewrite), \
             patch.object(model_tools.registry, "dispatch", side_effect=lambda _n, a, **_k: executed.append(dict(a)) or "ok"), \
             patch.object(tool_executor, "_dispatch_authorized_once", side_effect=AssertionError("agent dispatcher reached")):
            result = _default_dispatch("task")("synthetic", {"value": "before"})
        self.assertEqual(result, "ok")
        self.assertEqual(seen, [{"value": "before"}])
        self.assertEqual(executed, [{"value": "after"}])

    def test_agent_pipeline_rejects_a_second_dispatch(self):
        from agent import relay_tools, tool_executor as te
        from hermes_cli import middleware, plugins

        executed, errors = [], []
        agent = SimpleNamespace(
            _tool_guardrails=SimpleNamespace(before_call=lambda *_: SimpleNamespace(allows_execution=True)),
            tool_progress_callback=None,
            clarify_callback=None,
        )

        def relay(_name, args, dispatch, **_kwargs):
            result = dispatch(args)
            try:
                dispatch(args)
            except RuntimeError as exc:
                errors.append(str(exc))
            return result, args

        with patch.object(relay_tools, "execute", relay), \
             patch.object(middleware, "apply_tool_request_middleware", side_effect=lambda _n, a, **_k: SimpleNamespace(payload=a, trace=[])), \
             patch.object(middleware, "run_tool_execution_middleware", side_effect=lambda _n, a, dispatch, **_k: dispatch(a)), \
             patch.object(plugins, "_dispatch_pre_tool_call_hooks", return_value=(None, None)), \
             patch.object(te, "_begin_tool_execution"), \
             patch.object(te, "_run_with_activity_heartbeat", side_effect=lambda _a, _n, fn: fn()):
            result = te._run_agent_tool_execution_middleware(
                agent, function_name="synthetic", function_args={"value": "one"},
                effective_task_id="task", tool_call_id="call",
                execute=lambda args: executed.append(dict(args)) or "ok",
            )
        self.assertEqual(result.result, "ok")
        self.assertEqual(executed, [{"value": "one"}])
        self.assertEqual(len(errors), 1)
        self.assertIn("more than once", errors[0])

    def test_terminal_retries_after_uncertain_execution(self):
        """Stock Hermes may repeat an effect if execution succeeded but its acknowledgement failed."""
        import json
        from tools import terminal_tool

        executions = []

        class LostAcknowledgement:
            def execute(self, command, **kwargs):
                executions.append((command, kwargs["cwd"]))
                raise RuntimeError("lost acknowledgement")

        plan = SimpleNamespace(env_type="docker", effective_task_id="task", effective_timeout=1,
                               cwd="/synthetic", host_cwd=None)
        with patch.object(terminal_tool, "_resolve_command_cwd", return_value="/synthetic"), \
             patch.object(terminal_tool, "_yield_kwargs", return_value={}), \
             patch.object(terminal_tool.time, "sleep"), \
             patch.object(terminal_tool.logger, "warning"), \
             patch.object(terminal_tool.logger, "error"):
            result = terminal_tool._run_foreground(
                "synthetic effect", LostAcknowledgement(), plan,
                task_id="task", session_id="session", session_key="session", cwd_key="session",
                workdir=None, approval_note=None, clear_interrupt=False, metered=False,
            )
        self.assertEqual(executions, [("synthetic effect", "/synthetic")] * 4)
        self.assertIn("lost acknowledgement", json.loads(result)["error"])
