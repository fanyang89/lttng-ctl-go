#!/usr/bin/env python3
"""Independently compare the installed C AST with the checked-in API inventory."""

import json
from pathlib import Path
import subprocess

root = Path(__file__).resolve().parent.parent
result = subprocess.run(
    ["clang", "-Xclang", "-ast-dump=json", "-fsyntax-only", "-x", "c", "-"],
    input="#include <lttng/lttng.h>\n",
    text=True,
    capture_output=True,
    check=True,
)
functions = set()
endpoints = set()


def visit(node):
    name = node.get("name", "")
    if node.get("kind") == "FunctionDecl" and name.startswith("lttng_"):
        functions.add(name)
    if node.get("kind") == "VarDecl" and name.endswith("_endpoint"):
        endpoints.add(name)
    for child in node.get("inner", []):
        visit(child)


visit(json.loads(result.stdout))
bindings = {
    line.split("\t")[0]
    for line in (root / "ctl/API.tsv").read_text().splitlines()
    if not line.startswith("#")
}
if functions != bindings:
    raise SystemExit(
        f"Missing: {sorted(functions - bindings)}; unexpected: {sorted(bindings - functions)}"
    )
assert endpoints == {
    "lttng_session_daemon_notification_endpoint",
    "lttng_session_daemon_command_endpoint",
}
print(
    f"C AST coverage: {len(bindings)}/{len(functions)} public functions; {len(endpoints)} global endpoints"
)
