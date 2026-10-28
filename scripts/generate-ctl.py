#!/usr/bin/env python3
"""Generate complete typed control bindings from the installed public headers.

No network access or third-party Python modules are required. --check detects
header drift and stale generated bindings, including enums and declarations.
"""

import argparse
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parent.parent
ACRONYMS = {
    x: x.upper()
    for x in "id ids pid uid gid ppid vpid vtid vppid euid suid egid sgid vuid veuid vsuid vgid vegid vsgid ust jul url urls cpu ip fd elf sdt xml abi api".split()
}


def camel(value):
    return "".join(
        ACRONYMS.get(x.lower(), x.lower().capitalize())
        for x in value.removeprefix("lttng_").removeprefix("LTTNG_").split("_")
    )


def clean(text):
    return re.sub(r"/\*.*?\*/|//[^\n]*", "", text, flags=re.S)


def parse(include):
    pending = [include / "lttng/lttng.h"]
    headers = {}
    while pending:
        path = pending.pop()
        if path in headers:
            continue
        text = path.read_text()
        headers[path] = text
        pending += [
            include / x for x in re.findall(r'#include\s*[<"](lttng/[^>"]+)[>"]', text)
        ]
    funcs, enums, constants, globals_ = [], {}, {}, []
    for path, original in sorted(headers.items()):
        text = clean(original)
        header = str(path.relative_to(include))
        for name, body in re.findall(r"enum\s+(\w+)\s*\{([^}]+)\}", text, re.S):
            names = []
            for item in body.split(","):
                match = re.match(r"\s*(\w+)\s*(?:=.*)?$", item, re.S)
                if match:
                    names.append(match[1])
                    constants[match[1]] = name
            enums[name] = names
        for decl in re.findall(r"LTTNG_EXPORT\s+(?:extern\s+)?([^;]+);", text, re.S):
            decl = re.sub(r"\b(?:LTTNG_EXPORT|extern)\b", "", decl)
            decl = " ".join(decl.split())
            match = re.fullmatch(r"(.*?)\b(lttng_\w+)\s*\((.*?)\)", decl)
            if not match:
                if "(" in decl:
                    raise ValueError(
                        "Unrecognized public function declaration: " + decl
                    )
                globals_.append((header, decl))
                continue
            ret, name, args = match.groups()
            params = []
            for arg in [] if args == "void" else args.split(","):
                m = re.fullmatch(r"(.*?)\b(\w+)\s*", arg.strip())
                if not m:
                    raise ValueError((name, arg))
                params.append((m[1].strip(), m[2]))
            funcs.append(
                dict(
                    ret=ret.strip(), name=name, params=params, header=header, decl=decl
                )
            )
    return funcs, enums, constants, globals_


SCALARS = {
    "int": ("int", "C.int"),
    "unsigned int": ("uint32", "C.uint"),
    "unsigned long long": ("uint64", "C.ulonglong"),
    "uint64_t": ("uint64", "C.uint64_t"),
    "int64_t": ("int64", "C.int64_t"),
    "uint32_t": ("uint32", "C.uint32_t"),
    "int32_t": ("int32", "C.int32_t"),
    "uint16_t": ("uint16", "C.uint16_t"),
    "size_t": ("uint64", "C.size_t"),
    "pid_t": ("int32", "C.pid_t"),
    "uid_t": ("uint32", "C.uid_t"),
    "gid_t": ("uint32", "C.gid_t"),
    "double": ("float64", "C.double"),
    "bool": ("bool", "C.bool"),
}


def base(type_):
    return " ".join(type_.replace("*", " ").replace("const", "").split())


def scalar(type_):
    b = base(type_)
    if b.startswith("enum "):
        return camel(b[5:]), "C.enum_" + b[5:]
    return SCALARS[b]


def struct(type_):
    return base(type_).removeprefix("struct ")


def objectname(s):
    return "Calibration" if s == "lttng_calibrate" else camel(s)


OWNED_OUT = {
    "lttng_clear_session",
    "lttng_destroy_session_ext",
    "lttng_rotate_session",
    "lttng_session_list_rotation_schedules",
    "lttng_snapshot_list_output",
    "lttng_session_get_tracker_handle",
    "lttng_list_triggers",
    "lttng_error_query_execute",
    "lttng_notification_channel_get_next_notification",
}
ARRAYS = {
    "lttng_list_sessions",
    "lttng_list_domains",
    "lttng_list_channels",
    "lttng_list_events",
    "lttng_list_tracepoints",
    "lttng_list_tracepoint_fields",
    "lttng_list_syscalls",
}
TRANSFERS = {
    "lttng_action_snapshot_session_set_output": ["output"],
    "lttng_event_expr_array_field_element_create": ["array_field_expr"],
    "lttng_condition_event_rule_matches_append_capture_descriptor": ["expr"],
    "lttng_userspace_probe_location_function_create": ["lookup_method"],
    "lttng_userspace_probe_location_tracepoint_create": ["lookup_method"],
    "lttng_event_set_userspace_probe_location": ["location"],
}
OPTIONAL = {
    ("lttng_rotate_session", "descriptor"),
    ("lttng_snapshot_record", "output"),
    ("lttng_create_handle", "domain"),
}


def generate(include):
    funcs, enums, constants, globals_ = parse(include)
    names = {f["name"] for f in funcs}
    objects = {
        struct(t)
        for f in funcs
        for t in [f["ret"]] + [t for t, n in f["params"]]
        if "struct " in t
    }
    objects.add("lttng_endpoint")
    destroy = {s: s + "_destroy" for s in objects if s + "_destroy" in names}
    destroy["lttng_handle"] = "lttng_destroy_handle"

    def disposer(s):
        if s in destroy:
            return (
                f"func(p unsafe.Pointer) {{ C.ctlgo_{destroy[s]}((*C.struct_{s})(p)) }}"
            )
        return "func(p unsafe.Pointer) { C.free(p) }"

    lines = [
        "// Code generated by scripts/generate-ctl.py; DO NOT EDIT.",
        "//go:build linux && cgo",
        "",
        "package ctl",
        "",
        "/*",
        "#cgo CFLAGS: -Wno-deprecated-declarations",
        "#include <stdlib.h>",
        '#include "../internal/ctlstate/bridge_generated.h"',
        "*/",
        'import "C"',
        "",
        'import ("context"; "fmt"; "unsafe"; "github.com/fanyang89/lttng-ctl-go/internal/ctlstate")',
        "",
    ]
    for name in sorted(enums):
        lines += [
            f"// {camel(name)} corresponds to enum {name}.",
            f"type {camel(name)} int",
            "const (",
        ]
        lines += [f"{camel(v)} {camel(name)} = C.{v}" for v in enums[name]]
        lines += [")", ""]
    for s in sorted(objects):
        typ = objectname(s)
        lines += [
            f"// {typ} wraps a {s}. See package documentation for ownership.",
            f"type {typ} struct {{ ref *resource }}",
            f"func (obj *{typ}) resource() *resource {{ if obj == nil {{ return nil }}; return obj.ref }}",
            f"func (obj *{typ}) Close() error {{ if err := ctlstate.Lock(context.Background()); err != nil {{ return err }}; defer ctlstate.Unlock(); obj.resource().close(); return ctlstate.CallError() }}",
            "",
        ]
    tests = [
        "// Code generated by scripts/generate-ctl.py; DO NOT EDIT.",
        "//go:build linux && cgo",
        "package ctl",
        'import ("context"; "errors"; "testing")',
        "func TestEveryAPICancelsBeforeNativeCall(t *testing.T) { ctx,cancel := context.WithCancel(t.Context()); cancel()",
    ]
    unavailable_tests = ["func TestUnavailableAPIs(t *testing.T) { ctx := t.Context()"]
    manifest = [
        "# liblttng-ctl 2.14 public functions: C symbol\tGo binding\tHeader\tC declaration"
    ]
    for f in funcs:
        ret, name, params = f["ret"], f["name"], f["params"]
        go = camel(name)
        if name in destroy.values():
            _, arg = params[0]
            typ = objectname(struct(params[0][0]))
            lines += [
                f"// {go} wraps {name}. Close is an idempotent equivalent.",
                f"func {go}(ctx context.Context, arg *{typ}) error {{ if err := ctlstate.Lock(ctx); err != nil {{ return err }}; defer ctlstate.Unlock(); arg.resource().close(); return ctlstate.CallError() }}",
                "",
            ]
            tests += [
                f't.Run("{name}",func(t *testing.T) {{ if err := {go}(ctx,nil); !errors.Is(err,context.Canceled) {{ t.Fatal(err) }} }})'
            ]
            manifest.append("\t".join([name, go, f["header"], f["decl"]]))
            continue
        inputs, outputs, before, callargs, after, zeros, inputzero = (
            [],
            [],
            [],
            [],
            [],
            [],
            [],
        )
        parent = next(
            (n for t, n in params if "struct " in t and t.count("*") == 1), None
        )
        mutating = (
            bool(
                re.search(
                    r"_(set_|add_|remove_|append_|enable_|disable_|register|unregister|query)",
                    name,
                )
            )
            or name == "lttng_process_attr_tracker_handle_get_inclusion_set"
        )
        transfers = TRANSFERS.get(name, [])
        skip = set()
        if name == "lttng_action_path_create":
            skip.add("index_count")
        if name == "lttng_enable_event_with_exclusions":
            skip.add("event_name_exclusion_count")
        for t, n in params:
            v = "arg_" + n
            ptrs = t.count("*")
            b = base(t)
            if n in skip:
                seq = (
                    "arg_indexes" if n == "index_count" else "arg_event_name_exclusions"
                )
                cast = "C.size_t" if n == "index_count" else "C.int"
                callargs.append(f"{cast}(len({seq}))")
            elif name == "lttng_action_path_create" and n == "indexes":
                inputs.append(v + " []uint64")
                inputzero.append("nil")
                before += [
                    f"var native_{n} *C.uint64_t",
                    f"if len({v}) > 0 {{ native_{n} = (*C.uint64_t)(C.calloc(C.size_t(len({v})),8)); if native_{n} == nil {{ return FAIL ErrNull }}; defer C.free(unsafe.Pointer(native_{n})); for i,x := range {v} {{ unsafe.Slice(native_{n},len({v}))[i] = C.uint64_t(x) }} }}",
                ]
                callargs.append("native_" + n)
            elif (
                name == "lttng_enable_event_with_exclusions"
                and n == "event_name_exclusions"
            ):
                inputs.append(v + " []string")
                inputzero.append("nil")
                before += [
                    f"if uint64(len({v})) > 2147483647 {{ return FAIL ErrInvalid }}",
                    f"var native_{n} **C.char",
                    f"if len({v}) > 0 {{ native_{n} = (**C.char)(C.calloc(C.size_t(len({v})),C.size_t(unsafe.Sizeof(uintptr(0))))); if native_{n} == nil {{ return FAIL ErrNull }}; defer C.free(unsafe.Pointer(native_{n})); for i,x := range {v} {{ p,e := cstring(&x); if e != nil {{ return FAIL e }}; defer C.free(unsafe.Pointer(p)); unsafe.Slice(native_{n},len({v}))[i]=p }} }}",
                ]
                callargs.append("native_" + n)
            elif b == "char" and ptrs == 1:
                inputs.append(v + " *string")
                inputzero.append("nil")
                before += [
                    f"native_{n}, err := cstring({v}); if err != nil {{ return FAIL err }}",
                    f"defer C.free(unsafe.Pointer(native_{n}))",
                ]
                callargs.append("native_" + n)
            elif b == "char" and ptrs == 2:
                outputs.append(("*string", f"copyCString(native_{n})"))
                zeros.append("nil")
                before.append(f"var native_{n} *C.char")
                callargs.append("&native_" + n)
            elif b.startswith("struct ") and ptrs == 1:
                s = struct(t)
                typ = objectname(s)
                inputs.append(v + " *" + typ)
                inputzero.append("nil")
                write = mutating and "const" not in t
                # Ownership can only be transferred from an owned, writable object.
                before.append(f"var native_{n} *C.struct_{s}")
                validation = f"if err := {v}.resource().check({str(write or n in transfers).lower()}); err != nil {{ return FAIL err }}"
                if n in transfers:
                    validation += (
                        f"; if {v}.ref.parent != nil {{ return FAIL ErrReadOnly }}"
                    )
                if (name, n) in OPTIONAL:
                    before.append(
                        f"if {v} != nil {{ {validation}; native_{n} = (*C.struct_{s})({v}.ref.ptr) }}"
                    )
                else:
                    before += [validation, f"native_{n} = (*C.struct_{s})({v}.ref.ptr)"]
                if write:
                    after.append(f"{v}.ref.revision++")
                callargs.append("native_" + n)
            elif b.startswith("struct ") and ptrs == 2:
                s = struct(t)
                typ = objectname(s)
                before.append(f"var native_{n} *C.struct_{s}")
                callargs.append("&native_" + n)
                if name in ARRAYS:
                    outputs.append((f"*List[*{typ}]", f"list_{n}"))
                    zeros.append("nil")
                    after += [
                        f"owner_{n} := own(unsafe.Pointer(native_{n}),func(p unsafe.Pointer) {{ C.free(p) }})",
                        f"list_{n} := &List[*{typ}]{{ref:owner_{n}}}",
                        f"for i := range int(result) {{ list_{n}.items = append(list_{n}.items,&{typ}{{ref:borrow(unsafe.Pointer(&unsafe.Slice(native_{n},int(result))[i]),owner_{n},false)}}) }}",
                    ]
                else:
                    outputs.append(("*" + typ, f"out_{n}"))
                    zeros.append("nil")
                    if name in OWNED_OUT:
                        expr = f"own(unsafe.Pointer(native_{n}),{disposer(s)})"
                    else:
                        expr = f"borrow(unsafe.Pointer(native_{n}),arg_{parent}.resource(),{str('const' in t).lower()})"
                    after.append(
                        f"var out_{n} *{typ}; if native_{n} != nil {{ out_{n} = &{typ}{{ref:{expr}}} }}"
                    )
            elif ptrs == 2:
                assert name == "lttng_list_tracker_pids", (name, t)
                before.append(f"var native_{n} *C.int32_t")
                callargs.append("&native_" + n)
                outputs.append(("[]int32", f"out_{n}"))
                zeros.append("nil")
                after += [
                    f"defer C.free(unsafe.Pointer(native_{n}))",
                    f"out_{n} := make([]int32,int(native_nr_pids)); for i,x := range unsafe.Slice(native_{n},int(native_nr_pids)) {{ out_{n}[i]=int32(x) }}",
                ]
            elif ptrs == 1:
                gt, ct = scalar(t)
                before.append(f"var native_{n} {ct}")
                callargs.append("&native_" + n)
                outputs.append((gt, f"{gt}(native_{n})"))
                zeros.append("false" if gt == "bool" else "0")
            else:
                gt, ct = scalar(t)
                inputs.append(v + " " + gt)
                inputzero.append("false" if gt == "bool" else "0")
                if gt == "int" or b.startswith("enum "):
                    before.append(
                        f"if int64({ct}({v})) != int64({v}) {{ return FAIL ErrInvalid }}"
                    )
                if b == "size_t":
                    before.append(
                        f"if uint64({ct}({v})) != uint64({v}) {{ return FAIL ErrInvalid }}"
                    )
                callargs.append(f"{ct}({v})")
        retout, retzero = [], []
        if ret != "void":
            if base(ret) == "char" and "*" in ret:
                retout = [("*string", "copyCString(result)")]
                retzero = ["nil"]
            elif "struct " in ret:
                s = struct(ret)
                typ = objectname(s)
                retout = [("*" + typ, "out")]
                retzero = ["nil"]
                if (
                    name.endswith("_create")
                    or "_create_" in name
                    or name == "lttng_create_handle"
                ):
                    expr = f"own(unsafe.Pointer(result),{disposer(s)})"
                else:
                    expr = f"borrow(unsafe.Pointer(result),arg_{parent}.resource(),{str('const' in ret).lower()})"
                after += [
                    "if result == nil { return FAIL ErrNull }",
                    f"out := &{typ}{{ref:{expr}}}",
                ]
            else:
                gt, ct = scalar(ret)
                retout = [(gt, f"{gt}(result)")]
                retzero = ["false" if gt == "bool" else "0"]
        outputtypes = [x[0] for x in retout + outputs] + ["error"]
        fail = ", ".join(retzero + zeros)
        fail = (fail + ", ") if fail else ""
        lines += [
            f"// {go} wraps {name} ({f['header']}).",
            f"func {go}(ctx context.Context{', ' if inputs else ''}{', '.join(inputs)}) ({', '.join(outputtypes)}) {{",
            f"if err := ctlstate.Lock(ctx); err != nil {{ return {fail}err }}",
            "defer ctlstate.Unlock()",
        ]
        required = [name]
        # Preflight destructors before acquiring native-owned results.
        for t, n in [(ret, "")] + params:
            if "struct " in t and (not n or t.count("*") == 2 and name in OWNED_OUT):
                s = struct(t)
                if s in destroy and destroy[s] not in required:
                    required.append(destroy[s])
        symbols = ", ".join('"' + symbol + '"' for symbol in required)
        lines.append(
            f"if err := ctlstate.Require({symbols}); err != nil {{ return {fail}err }}"
        )
        lines += [
            x.replace("FAIL ", fail).replace("ErrInvalid", "ErrInvalidArgument")
            for x in before
        ]
        call = f"C.ctlgo_{name}({', '.join(callargs)})"
        lines += [(("result := " if ret != "void" else "") + call)]
        lines.append(
            f"if err := ctlstate.CallError(); err != nil {{ return {fail}err }}"
        )
        if (
            ret == "int"
            or ret.startswith("enum ")
            and ("_status" in ret or ret == "enum lttng_error_code")
        ):
            family = ret.removeprefix("enum ")
            lines.append(
                f'if err := statusError("{name}","{family}",int(result),{str(ret == "enum lttng_error_code").lower()}); err != nil {{ return {fail}err }}'
            )
        lines += [x.replace("FAIL ", fail) for x in after]
        if name == "lttng_trigger_create":
            lines.append(
                "out.ref.retained = []*resource{arg_condition.ref.retain(), arg_action.ref.retain()}"
            )
        lines += [f"arg_{n}.ref.moved()" for n in transfers]
        lines += [
            "return " + ", ".join([x[1] for x in retout + outputs] + ["nil"]),
            "}",
            "",
        ]
        tests.append(
            f't.Run("{name}",func(t *testing.T) {{ {", ".join(["_"] * (len(outputtypes) - 1) + ["err"])} := {go}(ctx{", " if inputzero else ""}{", ".join(inputzero)}); if !errors.Is(err,context.Canceled) {{ t.Fatal(err) }} }})'
        )
        unavailable_tests.append(
            f't.Run("{name}",func(t *testing.T) {{ present,err := HasSymbol(ctx,"{name}"); if err != nil {{ t.Fatal(err) }}; if present {{ return }}; '
            f"{', '.join(['_'] * (len(outputtypes) - 1) + ['err'])} = {go}(ctx{', ' if inputzero else ''}{', '.join(inputzero)}); "
            'if !errors.Is(err,ErrUnsupported) { t.Fatalf("missing API: %v",err) } })'
        )
        manifest.append("\t".join([name, go, f["header"], f["decl"]]))
    for name in [
        "lttng_session_daemon_notification_endpoint",
        "lttng_session_daemon_command_endpoint",
    ]:
        go = camel(name)
        lines += [
            f"// {go} returns the library-owned endpoint.",
            f'func {go}(ctx context.Context) (*Endpoint,error) {{ if err := ctlstate.Lock(ctx); err != nil {{ return nil,err }}; defer ctlstate.Unlock(); if err := ctlstate.Require("{name}"); err != nil {{ return nil,err }}; return &Endpoint{{ref:borrow(unsafe.Pointer(C.ctlgo_get_{name}()),nil,true)}},nil }}',
        ]
    lines += [
        "func copyCString(p *C.char) *string { if p == nil { return nil }; return new(C.GoString(p)) }",
        "var _ = fmt.Errorf",
    ]
    tests += ["}"]
    tests += unavailable_tests + ["}"]
    version = subprocess.check_output(
        ["pkg-config", "--modversion", "lttng-ctl"], text=True
    ).strip()
    if not version.startswith("2.14."):
        raise ValueError("Expected liblttng-ctl 2.14.x, got " + version)
    return {
        "ctl/api_generated.go": "\n".join(lines) + "\n",
        "ctl/api_generated_test.go": "\n".join(tests) + "\n",
        "ctl/API.tsv": "\n".join(manifest) + "\n",
        **generate_bridge(funcs),
    }


def generate_bridge(funcs):
    header = [
        "/* Code generated by scripts/generate-ctl.py; DO NOT EDIT. */",
        "#ifndef CTLGO_BRIDGE_GENERATED_H",
        "#define CTLGO_BRIDGE_GENERATED_H",
        "#include <lttng/lttng.h>",
        '#include "loader.h"',
    ]
    source = [
        "//go:build linux && cgo",
        "/* Code generated by scripts/generate-ctl.py; DO NOT EDIT. */",
        '#include "bridge_generated.h"',
    ]
    for f in funcs:
        ret, name, params = f["ret"], f["name"], f["params"]
        args = ", ".join(t + " " + n for t, n in params) or "void"
        types = ", ".join(t for t, n in params) or "void"
        values = ", ".join(n for t, n in params)
        signature = f"{ret} ctlgo_{name}({args})"
        header.append(signature + ";")
        fallback = "" if ret == "void" else " NULL" if "*" in ret else f" ({ret}) -1"
        source += [
            signature + " {",
            f"    typedef {ret} (*function_type)({types});",
            f'    function_type function = (function_type) ctlgo_require("{name}");',
            f"    if (!function) {{ return{fallback}; }}",
            f"    {'return ' if ret != 'void' else ''}function({values});",
            "}",
        ]
    for name in [
        "lttng_session_daemon_notification_endpoint",
        "lttng_session_daemon_command_endpoint",
    ]:
        signature = f"struct lttng_endpoint *ctlgo_get_{name}(void)"
        header.append(signature + ";")
        source += [
            signature + " {",
            f'    struct lttng_endpoint **value = ctlgo_require("{name}");',
            "    return value ? *value : NULL;",
            "}",
        ]
    header += ["#endif"]
    return {
        "internal/ctlstate/bridge_generated.h": "\n".join(header) + "\n",
        "internal/ctlstate/bridge_generated.c": "\n".join(source) + "\n",
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--include", default="/usr/include", type=Path)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    files = generate(args.include)
    for path, text in files.items():
        if path.endswith(".go"):
            text = subprocess.run(
                ["gofmt"], input=text, text=True, capture_output=True, check=True
            ).stdout
        target = ROOT / path
        if args.check:
            if not target.exists() or target.read_text() != text:
                sys.exit("Stale or missing generated file: " + path)
        else:
            target.write_text(text)
    print(
        "Verified" if args.check else "Generated",
        len(files["ctl/API.tsv"].splitlines()) - 1,
        "public functions",
    )


if __name__ == "__main__":
    main()
