# Contributing to CRI-O Credential Provider

Thanks for your interest in contributing to this project! We follow the
[Kubernetes contributor guidelines](https://git.k8s.io/community/contributors/guide#your-first-contribution).

## Getting Started

1. Fork and clone the repository
2. Create a new branch for your changes
3. Make your changes and ensure all checks pass
4. Submit a pull request

## Requirements

- All commits must be signed off (`git commit -s`) per the
  [Developer Certificate of Origin (DCO)](https://developercertificate.org)
- Code must pass linting: `make lint`
- All tests must pass: `make test`
- New functionality should include tests
- Shell scripts must pass `make shellcheck` and `make shfmt`
- Update documentation as needed

## Development

See the [README](README.md#development) for details on building, testing, and
linting.

## Reporting Bugs

Please use [GitHub Issues](https://github.com/cri-o/crio-credential-provider/issues/new)
to report bugs. Include steps to reproduce, expected behavior, and actual
behavior.

## Security Issues

For security vulnerabilities, please see [SECURITY.md](SECURITY.md).
