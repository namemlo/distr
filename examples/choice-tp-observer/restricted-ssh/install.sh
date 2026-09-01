#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ] || [ "$#" -lt 3 ] || [ "$#" -gt 4 ]; then
  echo "usage: sudo install.sh OBSERVER_PUBLIC_KEY CUSTOMER_STATE TRANSACTION_STATE [TARGET_USER]" >&2
  exit 2
fi

PUBLIC_KEY_FILE=$1
CUSTOMER_STATE_FILE=$2
TRANSACTION_STATE_FILE=$3
TARGET_USER=${4:-emlo-admin}
SCRIPT_DIRECTORY=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
TARGET_HOME=$(getent passwd "$TARGET_USER" | awk -F: 'NR == 1 {print $6}')
[ -n "$TARGET_HOME" ] || { echo "target user is missing" >&2; exit 1; }

PUBLIC_KEY=$(sed -n '1p' "$PUBLIC_KEY_FILE")
[ "$(wc -l < "$PUBLIC_KEY_FILE" | tr -d ' ')" -eq 1 ] || { echo "public key file must contain one line" >&2; exit 1; }
printf '%s\n' "$PUBLIC_KEY" | grep -Eq '^ssh-ed25519 [A-Za-z0-9+/]+={0,2}([^A-Za-z0-9+/=].*)?$' || {
  echo "observer public key must be one unadorned Ed25519 key" >&2
  exit 1
}

/usr/bin/python3 "$SCRIPT_DIRECTORY/runtime_state.py" --validate-file customer-api "$CUSTOMER_STATE_FILE" >/dev/null
/usr/bin/python3 "$SCRIPT_DIRECTORY/runtime_state.py" --validate-file transaction-api "$TRANSACTION_STATE_FILE" >/dev/null

install -o root -g root -m 0755 "$SCRIPT_DIRECTORY/forced_command.py" /usr/local/libexec/choice-tp-observer-forced-command
install -o root -g root -m 0755 "$SCRIPT_DIRECTORY/runtime_state.py" /usr/local/libexec/choice-tp-observer-runtime-state
install -d -o root -g root -m 0755 /etc/choice-tp-observer/runtime-state
install -o root -g root -m 0444 "$CUSTOMER_STATE_FILE" /etc/choice-tp-observer/runtime-state/customer-api.json
install -o root -g root -m 0444 "$TRANSACTION_STATE_FILE" /etc/choice-tp-observer/runtime-state/transaction-api.json

install -d -o "$TARGET_USER" -g "$TARGET_USER" -m 0700 "$TARGET_HOME/.ssh"
AUTHORIZED_KEYS="$TARGET_HOME/.ssh/authorized_keys"
touch "$AUTHORIZED_KEYS"
chown "$TARGET_USER:$TARGET_USER" "$AUTHORIZED_KEYS"
chmod 0600 "$AUTHORIZED_KEYS"
TEMPORARY=$(mktemp "$TARGET_HOME/.ssh/authorized_keys.XXXXXX")
trap 'rm -f "$TEMPORARY"' EXIT HUP INT TERM
grep -v ' choice-tp-independent-observer-v1$' "$AUTHORIZED_KEYS" > "$TEMPORARY" || true
printf 'restrict,command="/usr/bin/python3 /usr/local/libexec/choice-tp-observer-forced-command" %s choice-tp-independent-observer-v1\n' "$PUBLIC_KEY" >> "$TEMPORARY"
chown "$TARGET_USER:$TARGET_USER" "$TEMPORARY"
chmod 0600 "$TEMPORARY"
mv "$TEMPORARY" "$AUTHORIZED_KEYS"
trap - EXIT HUP INT TERM

echo "installed restricted Choice TP observer key and sealed runtime-state files"
