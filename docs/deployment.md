# Deployment

This guide covers building GoBot, running it directly, installing the
systemd service, and using Docker.

## Requirements

- Go `1.26.5+` or a newer supported release
- a writable filesystem for the BoltDB database
- network access to the IRC servers
- optionally, Docker and Docker Compose

## Build and run directly

From the repository root:

```sh
make build
```

The binary is written to `bin/irc-bot`. The helper script uses the same
Makefile target:

```sh
./scripts/build.sh
```

Run from the repository root so the default `config.yaml` is found:

```sh
./bin/irc-bot
```

For development:

```sh
make run
make test
```

Configuration-only changes require a restart. Code changes require a new
build before restarting.

## First-run workflow

There is no setup wizard yet. GoBot is configuration-first:

1. Review `config.yaml`.
2. Add at least one network, nickname, and channel.
3. Copy `.env.example` to `.env` and add secrets if needed.
4. Register and configure NickServ/SASL if the network requires it. For
   certificate authentication, place the client PEM on the host, register its
   fingerprint with NickServ, and configure SASL EXTERNAL.
5. Build with `make build`.
6. For a direct launch, export the environment file first:
   `set -a; . ./.env; set +a`; GoBot does not parse `.env` itself.
7. Start GoBot and watch its logs.
8. Confirm that it connects and joins the expected channels.
9. Test `!help`, `!weather`, `!wiki`, and `!karma`.
10. Invite it to a temporary channel with `/invite <bot-nick> #channel`, or
   add permanent channels to `config.yaml`.

Keep `.env` private:

```sh
chmod 600 .env
```

## systemd

The repository includes `deploy/systemd/gobot.service` and an installer. Run
the installer from the repository directory:

```sh
sudo ./scripts/install-systemd.sh
```

The installer detects the repository directory and invoking Linux user,
installs `/etc/systemd/system/gobot.service`, reloads systemd, and enables the
service at boot. It does not start GoBot, giving you time to finish the
configuration first.

Start it when ready:

```sh
sudo systemctl start gobot.service
sudo systemctl status gobot.service
journalctl -u gobot.service -f
```

The installer accepts a different deployment directory or service account:

```sh
sudo ./scripts/install-systemd.sh --install-dir /opt/gobot --user gobot --group gobot
```

If the repository moves, rerun the installer so the unit paths are regenerated.

## Docker

The repository includes a multi-stage `Dockerfile` and a Compose file.

Build and run manually:

```sh
docker build -t gobot .
docker run --rm --env-file .env gobot
```

Run with Compose:

```sh
cp .env.example .env
docker compose build
docker compose up -d
docker compose logs -f gobot
```

Compose mounts `config.yaml` read-only, persists data in a named volume,
publishes the stats listener, and restarts the bot unless stopped.

Useful commands:

```sh
docker compose ps
curl http://localhost:8082/stats
docker compose down
```

`docker compose down -v` also removes the named volume and deletes stored bot
data. Use it only when that data is no longer needed.
