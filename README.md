# CRI-O Credential Provider

<p align="center">
  <img src="./.github/logo.svg" alt="Logo" width="240">
</p>

This project aims to ship a credential provider built for CRI-O to authenticate
image pulls against registry mirrors by using namespaced Kubernetes Secrets.

## Features

- Seamless integration with CRI-O as a [kubelet image credential provider
  plugin](https://kubernetes.io/docs/tasks/administer-cluster/kubelet-credential-provider/)
- Authentication image pulls from registry mirrors using [Kubernetes
  Secrets](https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/#registry-secret-existing-credentials)
  scoped to namespaces
- Support for registry mirrors and pull-through caches
- Compatible with standard container registry authentication
- Works with both plain Kubernetes and OpenShift

## Building

To build the credential provider binary from source:

```bash
make
```

This will create the binary at `build/crio-credential-provider`.

You can also specify the target OS and architecture:

```bash
GOOS=linux GOARCH=amd64 make
```

To clean the build artifacts:

```bash
make clean
```

## Usage

### Running the main use case in plain Kubernetes

How to test the feature in Kubernetes is outlined in
[test/README.md](test/README.md).

### Running the main use case in OpenShift

How to test the feature in OpenShift is outlined in
[test/openshift/README.md](test/openshift/README.md).

## Development

### Running Tests

Run the unit tests:

```bash
make test
```

This will generate coverage reports in `build/coverprofile` and `build/coverage.html`.

### Linting

Run the Go linter:

```bash
make lint
```

Run shell script formatting:

```bash
make shfmt
```

Run shell script linting:

```bash
make shellcheck
```

### End-to-end Tests

The end-to-end tests are written using [bats](https://github.com/bats-core/bats-core) (Bash Automated Testing System).

To run the full test suite using a Vagrant machine:

```console
make e2e
```

Or run the tests locally for an already set-up cluster:

```console
cd test
bats e2e.bats
```

The test suite includes:

- **Mirror found test**: Validates that mirrors are correctly identified and auth files are written
- **No mirror found test**: Ensures proper handling when no mirrors are configured
- **No registries.conf test**: Validates behavior when registries.conf doesn't exist
- **Version test**: Validates the `--version` and `--version-json` flags

### Test Structure

- [`test/e2e.bats`](test/e2e.bats) - Main test file containing all test cases
- [`test/helpers.bash`](test/helpers.bash) - Shared helper functions used across tests
- [`test/e2e-run`](test/e2e-run) - Wrapper script that executes the bats test suite
- [`test/e2e-setup`](test/e2e-setup) - Initial setup script for configuring Kubernetes and CRI-O

### Verifying Dependencies

Check that all dependencies are up to date:

```bash
make dependencies
```

## Architecture

The credential provider implements the Kubernetes kubelet Credential Provider API
and integrates with CRI-O's image pull authentication flow. When the kubelet
needs to pull an image from a registry, it invokes this credential provider,
which:

1. Receives authentication requests via stdin ([kubelet Credential Provider
   API](https://kubernetes.io/docs/reference/config-api/kubelet-credentialprovider.v1/)).
1. Resolves matching mirrors from `/etc/containers/registries.conf` for the
   provided image from the request.
1. Finds mirror pull secrets in the Pods namespace by
   using the service account token from the request and the Kubernetes API.
1. Extracts the registry credentials from matching Secrets
1. Generates a short-lived authentication file for the image pull at
   `/etc/crio/auth/<NAMESPACE>-<IMAGE_NAME_SHA256>.json`, which includes mirror
   credentials, source registry credentials, and any global pull secrets.
1. Returns an empty `CredentialProviderResponse` to kubelet to indicate success.

This allows for secure, namespace-scoped credential management without exposing
credentials in node-level configuration files.

![flow-graph](.github/flow.jpg "Flow graph")

## Version Information

To display version information:

```bash
./build/crio-credential-provider --version
```

For JSON format:

```bash
./build/crio-credential-provider --version-json
```

## Contributing

Contributions are welcome! This project is part of the CRI-O ecosystem.

When contributing:

- Follow the existing code style
- Run `make lint` to ensure code quality
- Run `make test` to verify all tests pass
- Update documentation as needed

## Related Projects

- [CRI-O](https://github.com/cri-o/cri-o) - OCI-based Kubernetes Container Runtime Interface
- [Kubernetes](https://github.com/kubernetes/kubernetes) - Container orchestration platform
