#!/usr/bin/env bash

# Helper functions for e2e tests

# Global variables
SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)
AUTH_PATH=/etc/crio/auth
AUTH_PATH_IN_USE=$AUTH_PATH/in-use
REGISTRIES_CONF_PATH=/etc/containers/registries.conf

# Temporary file variables (set in setup)
GOT=""
EXPECTED=""
CRIO_LOGS=""

# Setup temporary files for a test
setup_temp_files() {
    GOT=$(mktemp)
    EXPECTED=$(mktemp)
    CRIO_LOGS=$(mktemp)
}

# Cleanup temporary files
cleanup_temp_files() {
    if [[ -n "$GOT" && -f "$GOT" ]]; then
        rm -f "$GOT"
    fi
    if [[ -n "$EXPECTED" && -f "$EXPECTED" ]]; then
        rm -f "$EXPECTED"
    fi
    if [[ -n "$CRIO_LOGS" && -f "$CRIO_LOGS" ]]; then
        rm -f "$CRIO_LOGS"
    fi
}

# Clear journald logs
clear_journald() {
    sudo journalctl --vacuum-time=1ms --rotate
}

# Prepare registries.conf file
# Usage: prepare_registries_conf <content|rm>
prepare_registries_conf() {
    if [[ $1 == rm ]]; then
        sudo rm -f "$REGISTRIES_CONF_PATH"
    else
        echo "$1" | sudo tee "$REGISTRIES_CONF_PATH" >/dev/null
    fi
    sudo systemctl restart crio
    clear_journald
}

# Run the test pod
run_test_pod() {
    kubectl delete -f "$SCRIPT_DIR/cluster/pod.yml" || true
    kubectl apply -f "$SCRIPT_DIR/cluster/pod.yml"
    kubectl wait --for=condition=ready pod/test-pod --timeout=120s
}

# Save logs from the last credential provider run
save_logs() {
    # Save and clean the logs from the last credential provider run
    LAST_PID=$(sudo journalctl -q -o cat --no-pager -n 1 --output-fields _PID _COMM=crio-credential)
    sudo journalctl _PID="$LAST_PID" | sed -E 's;.*.go:[0-9]+:\s(.*);\1;' >"$GOT"
}

# Get CRI-O logs once to avoid redundant journal queries
get_crio_logs() {
    # shellcheck disable=SC2024
    sudo journalctl -u crio >"$CRIO_LOGS"
}

# Assert that a pattern exists in CRI-O logs
assert_crio_log_contains() {
    local pattern="$1"
    grep -q "$pattern" "$CRIO_LOGS"
}

# Assert that a pattern does not exist in CRI-O logs
assert_crio_log_not_contains() {
    local pattern="$1"
    ! grep -q "$pattern" "$CRIO_LOGS"
}

# Assert that auth directories are empty
assert_auth_dirs_empty() {
    [[ $(find "$AUTH_PATH" -type f 2>/dev/null) == "" ]]
    test -d "$AUTH_PATH_IN_USE"
    [[ $(find "$AUTH_PATH_IN_USE" -type f 2>/dev/null) == "" ]]
}

# Assert registry authorization
assert_registry_authorized() {
    podman logs registry 2>&1 | grep -q "authorized request"
}
