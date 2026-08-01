# Development and CI

## Local checks

Run the same core checks used by CI:

~~~sh
gofmt -w ./bot ./cmd ./plugins ./storage
go vet ./...
go test -race ./...
go build ./cmd/irc-bot
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
~~~

The project also provides:

~~~sh
make build
make test
make run
./scripts/build.sh
~~~

Do not commit bin/irc-bot, local databases, .env, or host-specific
configuration. The repository's config.yaml is an example; deployment hosts
should keep their real values outside Git.

## GitHub Actions

GitHub Actions verifies pushes and pull requests targeting main or dev. The
workflow checks formatting, go vet, race-enabled tests, the production build,
and known vulnerabilities with govulncheck. A separate CodeQL workflow
performs source-level security analysis and publishes findings to GitHub code
scanning.

The workflow also runs weekly on Monday at 03:17 UTC. Dependabot checks Go
modules and GitHub Actions weekly and opens update pull requests labeled
dependencies and security. If you fork GoBot, review those updates before
merging or deploying them.

For branch protection, require pull requests for main and enable the checks
only after the workflows have reported successfully at least once. The check
names are workflow/job combinations shown by GitHub, such as GoBot CI / Verify
and CodeQL / Analyze Go.
