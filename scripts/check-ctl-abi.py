#!/usr/bin/env python3
"""Compare public ABI used by the SDK against older LTTng include directories.

Requires clang and a native C compiler. Each argument is an include directory
containing lttng/lttng.h; the installed headers are the build-time baseline.
"""

import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile

RECORDS = {
    "lttng_domain",
    "lttng_handle",
    "lttng_channel_attr",
    "lttng_channel",
    "lttng_event",
    "lttng_event_field",
    "lttng_event_context",
    "lttng_calibrate",
    "lttng_session",
    "lttng_event_probe_attr",
    "lttng_event_function_attr",
    "lttng_event_perf_counter_ctx",
}


def inspect(include):
    flags = ["-I" + str(include)]
    result = subprocess.run(
        ["clang", *flags, "-Xclang", "-ast-dump=json", "-fsyntax-only", "-x", "c", "-"],
        input="#include <lttng/lttng.h>\n",
        text=True,
        capture_output=True,
        check=True,
    )
    functions, expressions = {}, {}

    def visit(node):
        kind, name = node.get("kind"), node.get("name", "")
        if kind == "FunctionDecl" and name.startswith("lttng_"):
            # Const and enum spelling changes do not change the C calling ABI.
            signature = re.sub(r"\bconst\b|\brestrict\b", "", node["type"]["qualType"])
            signature = re.sub(r"enum \w+", "int", signature)
            functions[name] = re.sub(r"\s+", "", signature)
        if kind == "EnumConstantDecl" and name.startswith("LTTNG_"):
            expressions[name] = name
        if kind == "RecordDecl" and name in RECORDS and node.get("completeDefinition"):
            expressions[name + ".size"] = f"sizeof(struct {name})"
            expressions[name + ".align"] = f"_Alignof(struct {name})"
            for field in node.get("inner", []):
                if field.get("kind") == "FieldDecl" and field.get("name"):
                    member = field["name"]
                    if not member.startswith("padding"):
                        expressions[name + "." + member] = (
                            f"offsetof(struct {name}, {member})"
                        )
        for child in node.get("inner", []):
            visit(child)

    visit(json.loads(result.stdout))
    source = "#include <stddef.h>\n#include <stdio.h>\n#include <lttng/lttng.h>\nint main(void) {\n"
    for name, expr in sorted(expressions.items()):
        source += f'printf("{name}=%lld\\n", (long long) ({expr}));\n'
    source += "return 0; }\n"
    base = Path(os.environ.get("TMPDIR", str(Path.home() / "tmp"))) / "opencode"
    base.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(dir=base) as work:
        binary = str(Path(work) / "abi")
        subprocess.run(
            ["cc", *flags, "-x", "c", "-", "-o", binary],
            input=source,
            text=True,
            check=True,
        )
        values = dict(
            line.split("=", 1)
            for line in subprocess.check_output([binary], text=True).splitlines()
        )
    return functions, values


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline", type=Path, default=Path("/usr/include"))
    parser.add_argument("includes", type=Path, nargs="+")
    args = parser.parse_args()
    functions, values = inspect(args.baseline)
    failed = False
    for include in args.includes:
        old_functions, old_values = inspect(include)
        differences = []
        for label, current, old in [
            ("signature", functions, old_functions),
            ("layout/enum", values, old_values),
        ]:
            for name in sorted(current.keys() & old.keys()):
                if current[name] != old[name]:
                    # NR is a sentinel, not a wire value. Session arrays retain
                    # identical sizes/offsets and come from malloc (8-aligned).
                    # Error codes are deliberately returned in the native
                    # namespace; 2.10 used 139 for AGENT_TRACING_DISABLED.
                    if (
                        name == "LTTNG_ERR_NR"
                        or (
                            name == "lttng_session.align"
                            and (old[name], current[name]) == ("4", "8")
                            and old["lttng_session.size"]
                            == current["lttng_session.size"]
                            and int(current["lttng_session.size"]) % 8 == 0
                        )
                        or (
                            name == "LTTNG_ERR_AGENT_TRACING_DISABLED"
                            and (old[name], current[name]) == ("139", "137")
                        )
                    ):
                        print(
                            f"reviewed difference: {name}: {old[name]} -> {current[name]}"
                        )
                        continue
                    differences.append(
                        f"{label}: {name}: {old[name]} -> {current[name]}"
                    )
        print(
            f"{include}: {len(functions.keys() & old_functions.keys())} shared functions; "
            f"{len(values.keys() & old_values.keys())} shared layout/enum checks"
        )
        for difference in differences:
            print(difference)
        failed |= bool(differences)
    raise SystemExit(int(failed))


if __name__ == "__main__":
    main()
