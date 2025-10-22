#!/usr/bin/env bats

# E2E tests for crio-credential-provider

# Load helper functions
load helpers

# Setup function - runs once before all tests
setup_file() {
	# Change to the test directory
	cd "$BATS_TEST_DIRNAME" || exit 1

	# Start the local container registry
	registry/start

	# Setup the requirements and run initial cluster setup
	kubectl apply -f cluster/rbac.yml -f cluster/secret.yml
}

# Teardown function - runs once after all tests
teardown_file() {
	# Restore original registries.conf if it was backed up
	if [[ -f "$REGISTRIES_CONF_PATH.backup" ]]; then
		sudo mv "$REGISTRIES_CONF_PATH.backup" "$REGISTRIES_CONF_PATH"
		sudo systemctl restart crio
	fi
}

# Setup function - runs before each test
setup() {
	setup_temp_files
}

# Teardown function - runs after each test
teardown() {
	cleanup_temp_files
}

@test "mirror should be found and auth file written" {
	# Test variables
	local NAMESPACE=default
	local IMAGE_HASH=7e59ad64326bc321517fb6fc6586de5ee149178394d9edfa2a877176cdf6fad5
	local AUTH_FILE=$AUTH_PATH/$NAMESPACE-$IMAGE_HASH.json
	# AUTH_FILE_IN_USE has a UUID suffix, which we skip for validation
	local AUTH_FILE_IN_USE=$AUTH_PATH_IN_USE/$NAMESPACE-$IMAGE_HASH

	# Prepare registries.conf with mirror configuration
	prepare_registries_conf "$(cat "$BATS_TEST_DIRNAME/registries.conf")"

	# Run the test pod
	run_test_pod

	# Save credential provider logs
	save_logs

	# Expected output from credential provider
	cat > "$EXPECTED" << EOL
Running credential provider
Reading from stdin
Parsed credential provider request for image "docker.io/library/nginx"
Parsing namespace from request
Matching mirrors for registry config: $REGISTRIES_CONF_PATH
Got mirror(s) for "docker.io/library/nginx": "localhost:5000"
Getting secrets from namespace: $NAMESPACE
Unable to find env file "/etc/kubernetes/apiserver-url.env", using default API server host: localhost:6443
Got 1 secret(s)
Parsing secret: my-secret
Found docker config JSON auth in secret "my-secret" for "http://localhost:5000"
Checking if mirror "localhost:5000" matches registry "localhost:5000"
Using mirror auth "localhost:5000" for registry from secret "localhost:5000"
Wrote auth file to $AUTH_FILE with 1 number of entries
Auth file path: $AUTH_FILE
EOL

	# Assert credential provider logs match expected output
	run diff "$GOT" "$EXPECTED"
	[ "$status" -eq 0 ]

	# Get CRI-O logs once for efficiency
	get_crio_logs

	# Assert CRI-O logs contain expected messages
	assert_crio_log_contains "Using auth file for namespace $NAMESPACE: $AUTH_FILE"
	assert_crio_log_contains "Removed temp auth file: $AUTH_FILE_IN_USE"

	# Assert auth directories are empty (files cleaned up by CRI-O)
	assert_auth_dirs_empty

	# Assert registry was accessed with authorization
	assert_registry_authorized
}

@test "no mirror should be found with empty registries.conf" {
	# Prepare empty registries.conf (no mirrors configured)
	prepare_registries_conf ""

	# Run the test pod
	run_test_pod

	# Save credential provider logs
	save_logs

	# Expected output: no mirrors found
	cat > "$EXPECTED" << EOL
Running credential provider
Reading from stdin
Parsed credential provider request for image "docker.io/library/nginx"
Parsing namespace from request
Matching mirrors for registry config: $REGISTRIES_CONF_PATH
No mirrors found, will not write any auth file
EOL

	# Assert credential provider logs match expected output
	run diff "$GOT" "$EXPECTED"
	[ "$status" -eq 0 ]

	# Get CRI-O logs once for efficiency
	get_crio_logs

	# Assert CRI-O logs show it looked for auth files but didn't find any
	assert_crio_log_contains "Looking for namespaced auth JSON file in:"
	assert_crio_log_not_contains "Using auth file for namespace default"

	# Assert auth directories are empty
	[[ $(find "$AUTH_PATH" -type f 2> /dev/null) == "" ]]
}

@test "should stop when registries.conf does not exist" {
	# Remove registries.conf file
	prepare_registries_conf rm

	# Run the test pod
	run_test_pod

	# Save credential provider logs
	save_logs

	# Expected output: registries.conf does not exist
	cat > "$EXPECTED" << EOL
Running credential provider
Registries conf path "$REGISTRIES_CONF_PATH" does not exist, stopping
EOL

	# Assert credential provider logs match expected output
	run diff "$GOT" "$EXPECTED"
	[ "$status" -eq 0 ]

	# Get CRI-O logs once for efficiency
	get_crio_logs

	# Assert CRI-O logs show it looked for auth files but didn't find any
	assert_crio_log_contains "Looking for namespaced auth JSON file in:"
	assert_crio_log_not_contains "Using auth file for namespace default"
}

@test "version flag should display version information" {
	local BINARY_PATH="$BATS_TEST_DIRNAME/../build/crio-credential-provider"

	# Test --version flag
	run "$BINARY_PATH" --version
	[ "$status" -eq 0 ]
	[[ "$output" =~ "Version:" ]]

	# Test --version-json flag
	run bash -c "$BINARY_PATH --version-json | jq -e"
	[ "$status" -eq 0 ]
}
