#!/usr/bin/env python3

import os
import re
import shlex


COMPONENTS = {
    "customer-api": {
        "compose": "/home/emlo-admin/apps/remittance/dev/emlo-remittance-customer/docker-compose.yaml",
        "config": "/home/emlo-admin/apps/remittance/dev/emlo-remittance-customer/appsettings.Production.json",
        "paths": (
            "/customer-api/alive",
            "/customer-api/healthz",
            "/customer-api/swagger/v1/swagger.json",
            "/customer-api/.well-known/distr-runtime-state",
        ),
    },
    "transaction-api": {
        "compose": "/home/emlo-admin/apps/remittance/dev/emlo-remittance-transaction/docker-compose.yaml",
        "config": "/home/emlo-admin/apps/remittance/dev/emlo-remittance-transaction/appsettings.Production.json",
        "paths": (
            "/transaction-api/alive",
            "/transaction-api/healthz",
            "/transaction-api/swagger/v1/swagger.json",
            "/transaction-api/.well-known/distr-runtime-state",
        ),
    },
}
GATEWAY = "http://127.0.0.1:12000"
HOST_HEADER = "Host: api-gateway.dev.choice-tp.emlotech.com"
DIGEST = re.compile(r"^sha256:[0-9a-f]{64}$")


def allowed_commands():
    commands = []
    for component, values in COMPONENTS.items():
        commands.append(
            [
                "/usr/bin/docker",
                "inspect",
                "--type",
                "container",
                "--format",
                "{{.Image}}\t{{.Config.Image}}\t{{.State.Status}}",
                component,
            ]
        )
        commands.append(["/usr/bin/sha256sum", values["compose"], values["config"]])
        for probe_path in values["paths"]:
            if probe_path.endswith("distr-runtime-state"):
                commands.append(
                    [
                        "/usr/bin/curl",
                        "--silent",
                        "--show-error",
                        "--fail",
                        "--request",
                        "GET",
                        "--proto",
                        "=http",
                        "--connect-timeout",
                        "2",
                        "--max-time",
                        "5",
                        "--max-filesize",
                        "4096",
                        "--header",
                        "Accept: application/json",
                        "--header",
                        HOST_HEADER,
                        f"{GATEWAY}{probe_path}",
                    ]
                )
            else:
                commands.append(
                    [
                        "/usr/bin/curl",
                        "--silent",
                        "--show-error",
                        "--output",
                        "/dev/null",
                        "--write-out",
                        "%{http_code}",
                        "--max-time",
                        "8",
                        "--header",
                        HOST_HEADER,
                        f"{GATEWAY}{probe_path}",
                    ]
                )
        commands.append(
            [
                "/usr/bin/timeout",
                "--signal=KILL",
                "--kill-after=1s",
                "5s",
                "/usr/local/libexec/choice-tp-observer-runtime-state",
                "--component",
                component,
            ]
        )
    return commands


ALLOWED_COMMANDS = {tuple(command) for command in allowed_commands()}


def validate_command(command):
    if not command or "\x00" in command or "\r" in command or "\n" in command:
        raise ValueError("observer SSH command is missing or invalid")
    try:
        arguments = shlex.split(command, posix=True)
    except ValueError as error:
        raise ValueError("observer SSH command cannot be parsed") from error
    if tuple(arguments) in ALLOWED_COMMANDS:
        return arguments
    if (
        len(arguments) == 6
        and arguments[:5]
        == [
            "/usr/bin/docker",
            "image",
            "inspect",
            "--format",
            "{{json .RepoDigests}}\t{{.Os}}/{{.Architecture}}",
        ]
        and DIGEST.fullmatch(arguments[5])
    ):
        return arguments
    raise ValueError("observer SSH command is outside the sealed read-only allowlist")


def main():
    try:
        arguments = validate_command(os.environ.get("SSH_ORIGINAL_COMMAND", ""))
    except ValueError as error:
        raise SystemExit(str(error)) from None
    os.execv(arguments[0], arguments)


if __name__ == "__main__":
    main()
