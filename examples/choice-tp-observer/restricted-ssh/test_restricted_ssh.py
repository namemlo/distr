import importlib.util
import json
import os
import shlex
import stat
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parent


def load_module(name, file_name):
    spec = importlib.util.spec_from_file_location(name, ROOT / file_name)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


forced_command = load_module("forced_command", "forced_command.py")
runtime_state = load_module("runtime_state", "runtime_state.py")


class RestrictedSSHTests(unittest.TestCase):
    def test_allowlist_accepts_only_observer_generated_commands(self):
        for command in forced_command.ALLOWED_COMMANDS:
            rendered = shlex.join(command)
            self.assertEqual(list(command), forced_command.validate_command(rendered))
        image_command = shlex.join(
            [
                "/usr/bin/docker",
                "image",
                "inspect",
                "--format",
                "{{json .RepoDigests}}\t{{.Os}}/{{.Architecture}}",
                f"sha256:{'a' * 64}",
            ]
        )
        self.assertEqual("/usr/bin/docker", forced_command.validate_command(image_command)[0])

    def test_allowlist_rejects_shell_mutation_and_rebinding(self):
        rejected = (
            "'/bin/sh' '-c' 'id'",
            "'/usr/bin/docker' 'exec' 'customer-api' 'id'",
            "'/usr/bin/curl' 'https://example.invalid'",
            "'/usr/bin/sha256sum' '/etc/shadow'",
            "'/usr/bin/timeout' '5s' '/usr/local/libexec/other-helper'",
            "'/usr/bin/docker' 'inspect' 'customer-api'; rm -rf /",
        )
        for command in rejected:
            with self.assertRaises(ValueError):
                forced_command.validate_command(command)

    def test_runtime_state_is_exact_bounded_and_canonical(self):
        value = {
            "schemaVersion": "1.0.0",
            "capabilityChecksum": "sha256:" + "1" * 64,
            "topologyChecksum": "sha256:" + "3" * 64,
        }
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "customer.json"
            path.write_text(json.dumps(value), encoding="utf-8")
            os.chmod(path, 0o600 if os.name == "posix" else stat.S_IREAD)
            self.assertEqual(value, runtime_state.load_state("customer-api", str(path), False))
            if os.name != "posix":
                os.chmod(path, stat.S_IREAD | stat.S_IWRITE)
            path.write_text(json.dumps(value | {"secret": "rejected"}), encoding="utf-8")
            os.chmod(path, 0o600 if os.name == "posix" else stat.S_IREAD)
            with self.assertRaisesRegex(ValueError, "exactly the three approved fields"):
                runtime_state.load_state("customer-api", str(path), False)


if __name__ == "__main__":
    unittest.main()
