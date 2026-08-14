# Contributing to GoBot

Thanks for helping improve GoBot. Small, focused changes are easiest to
review and maintain.

## Before you start

- Read the relevant guide in [`docs/`](docs/).
- Do not commit `.env` files, API keys, passwords, local databases, or built
  binaries.
- Keep public examples generic; never add real credentials, private addresses,
  or deployment-only configuration.

## Local checks

GoBot currently targets Go `1.26.6` or newer. Before opening a pull request:

```sh
go test ./...
go vet ./...
go test -race ./plugins
git diff --check
make build
```

Use `gofmt` on changed Go files. If a change adds a plugin or configuration
option, include focused tests and update the relevant guide under `docs/`.

## Branches and pull requests

- Start from an up-to-date `main` branch.
- Use a focused branch name such as `feature/...`, `fix/...`, or `docs/...`.
- Explain what changed, how it was tested, and whether a config change or
  restart is required.
- Add only safe example values to the tracked `config.yaml`; keep real
  deployment values on the host.
- Open a pull request against `main` and wait for GoBot CI, CodeQL, and any
  required checks to finish before merging.
- Never force-push or bypass branch protection on `main`.

## Design and safety

- Keep IRC responses bounded so a command does not create a flood.
- Validate user input and use timeouts for external HTTP requests.
- Avoid new dependencies unless they solve a clear problem.
- Do not expose secrets in logs, metrics, or IRC responses.

Useful links:

- [Issues](https://github.com/variablenix/GoBot/issues)
- [Pull requests](https://github.com/variablenix/GoBot/pulls)
- [Actions](https://github.com/variablenix/GoBot/actions)
- [Security guidance](docs/security.md)
- [GitHub pull request documentation](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests)
