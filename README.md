# GoBot

GoBot is an extensible Go IRC bot for long-running use on one or more IRC
networks. It supports TLS/SASL authentication, multiple networks and
channels, persistent plugin data, rate-limited responses, games, reminders,
URL titles, and Prometheus metrics.

The repository contains example connection settings so you can see the
configuration shape. Replace them with the networks, channels, identity, and
secrets for your own deployment.

## Contents

- [What GoBot does](#what-gobot-does)
- [Quick start](#quick-start)
- [Documentation map](#documentation-map)
- [Project layout](#project-layout)
- [Contributing and CI](docs/development.md)
- [License](#license)

## What GoBot does

Each configured network connection:

- connects over TLS and can authenticate with SASL PLAIN
- joins any number of configured channels
- listens for channel, private, and invite events
- dispatches messages to enabled plugins
- reconnects with backoff after network failures
- paces outbound messages to avoid flooding

Stateful features store their data in a local BoltDB database. Runtime
counters are exposed through `/stats` and `/metrics`; cumulative counters
are also persisted so they survive restarts.

## Quick start

Requirements:

- Go `1.25.12+` or a newer supported release
- IRC network access
- a writable data directory

1. Review `config.yaml` and add your networks and channels.
2. Copy `.env.example` to `.env` and add secrets such as SASL or API keys.
3. Build the binary with `make build` or `./scripts/build.sh`.
4. Start it from the repository root with `./bin/irc-bot`.

For a service-managed deployment, use the [systemd and deployment
guide](docs/deployment.md). Docker and Docker Compose are also documented
there.

## Documentation map

| Guide | Covers |
| --- | --- |
| [Deployment](docs/deployment.md) | Build, run, systemd, Docker, and first-run workflow |
| [Configuration](docs/configuration.md) | Multi-network config, channels, invites, rate limits, and formatting |
| [Plugins](docs/plugins.md) | Built-in plugin behavior and command reference |
| [Games and activities](docs/games.md) | Blackjack, pool, polls, reminders, Duck Hunt, dice, and choices |
| [NickServ and SASL](docs/nickserv-sasl.md) | Nickname registration and secure authentication |
| [Banter and fortune](docs/banter-fortune.md) | Optional banter, quotes, and curated fortune files |
| [News](docs/news.md) | NewsAPI setup and commands |
| [Monitoring](docs/monitoring.md) | `/stats`, `/metrics`, Prometheus, and Grafana |
| [Security](docs/security.md) | Deployment and runtime security guidance |
| [Development and CI](docs/development.md) | Project workflow, tests, CI, CodeQL, and Dependabot |

## Project layout

```text
cmd/irc-bot/       application entrypoint
bot/               IRC connection, config, queue, stats, and interfaces
plugins/           built-in plugins and tests
data/foods/        local food, cuisine, and beer suggestion lists
data/sports.txt    local sports suggestion list
storage/           BoltDB wrapper used by stateful plugins
quotes/            built-in quote and response files
grafana/           importable Prometheus dashboard and preview
scripts/            build, publishing, and systemd helpers
deploy/systemd/    systemd unit template
```

The binary is generated at `bin/irc-bot`; it is a build artifact, not source
code. See [Development and CI](docs/development.md) for local checks.

## License

MIT
