#!/usr/bin/env python3

import argparse
import json
import os
import re
import stat


STATE_DIRECTORY = "/etc/choice-tp-observer/runtime-state"
COMPONENTS = {"customer-api", "transaction-api"}
DIGEST = re.compile(r"^sha256:[0-9a-f]{64}$")
SCHEMA = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$")
MAXIMUM_BYTES = 4096


def load_state(component, file_path=None, require_root_owner=True):
    if component not in COMPONENTS:
        raise ValueError("component is outside the sealed runtime-state allowlist")
    path = file_path or os.path.join(STATE_DIRECTORY, f"{component}.json")
    flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0)
    descriptor = os.open(path, flags)
    try:
        metadata = os.fstat(descriptor)
        if not stat.S_ISREG(metadata.st_mode):
            raise ValueError("runtime-state source must be a regular file")
        if require_root_owner and metadata.st_uid != 0:
            raise ValueError("runtime-state source must be root-owned")
        if metadata.st_mode & 0o022:
            raise ValueError("runtime-state source must not be group/other writable")
        data = os.read(descriptor, MAXIMUM_BYTES + 1)
    finally:
        os.close(descriptor)
    if not data or len(data) > MAXIMUM_BYTES:
        raise ValueError("runtime-state source size is invalid")
    try:
        value = json.loads(data.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise ValueError("runtime-state source is not valid UTF-8 JSON") from error
    if set(value) != {"schemaVersion", "capabilityChecksum", "topologyChecksum"}:
        raise ValueError("runtime-state source must contain exactly the three approved fields")
    if not SCHEMA.fullmatch(value.get("schemaVersion", "")):
        raise ValueError("runtime-state schemaVersion is invalid")
    for key in ("capabilityChecksum", "topologyChecksum"):
        if not DIGEST.fullmatch(value.get(key, "")):
            raise ValueError(f"runtime-state {key} is invalid")
    return value


def main():
    parser = argparse.ArgumentParser(add_help=False)
    parser.add_argument("--component", choices=sorted(COMPONENTS))
    parser.add_argument("--validate-file", nargs=2, metavar=("COMPONENT", "PATH"))
    arguments = parser.parse_args()
    if bool(arguments.component) == bool(arguments.validate_file):
        raise SystemExit("select exactly one runtime-state operation")
    try:
        if arguments.validate_file:
            component, file_path = arguments.validate_file
            value = load_state(component, file_path, require_root_owner=False)
        else:
            value = load_state(arguments.component)
    except (OSError, ValueError) as error:
        raise SystemExit(f"runtime-state unavailable: {error}") from None
    print(json.dumps(value, sort_keys=True, separators=(",", ":")))


if __name__ == "__main__":
    main()
