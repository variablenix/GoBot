# GoBot

GoBot is a Go IRC bot built for long-running use on one or more IRC networks. It connects over TLS, rejoins after disconnects, rate-limits outbound messages, stores plugin data in BoltDB, and exposes runtime stats over HTTP.

The repository includes sample IRC connection details to help you get started, but the bot is structured to support multiple networks and multiple channels per network.

## What GoBot does

GoBot starts one IRC connection per configured network. Each connection:

- authenticates with the configured nickname and optional SASL credentials
- joins the configured channels
- listens for channel and private messages
- hands each message to enabled plugins
- reconnects with backoff if the network drops

Persistent plugin state such as seen/tell/karma data is stored in BoltDB. Runtime counters are exposed on `/stats` and `/metrics`.

## Requirements

- Go `1.23+`
- A writable filesystem for the BoltDB database
- IRC network access
- Optional: Docker / Docker Compose
- Optional: fortune packages if you want extra banter quote sources

## Build and run

Build the bot binary:

```sh
make build
```

This writes:

```text
bin/irc-bot
```

You can also use the helper script:

```sh
./scripts/build.sh
```

Run directly from the repository root so `config.yaml` is found:

```sh
./bin/irc-bot
```

For development:

```sh
make run
make test
```

## Project layout

- `cmd/irc-bot/`: main program entrypoint
- `bot/`: IRC connection, config loading, queueing, stats, and plugin interfaces
- `plugins/`: built-in plugins, one plugin per file where practical
- `storage/`: BoltDB wrapper used by stateful plugins
- `quotes/`: local banter quote files
- `scripts/`: helper scripts for build/publish and optional runner setup

## Configuration overview

The main configuration file is `config.yaml`.

Important sections:

- `networks`: one entry per IRC network
- `networks[].server`: IRC host, port, TLS, certificate verification
- `networks[].identity`: nick, user, real name, optional SASL settings
- `networks[].channels`: channels to join on that network
- `command_prefix`: command prefix, default `!`
- `rate_limit`: outbound message pacing
- `plugins`: plugin enable/disable and plugin-specific settings
- `stats`: HTTP stats listener
- `storage`: BoltDB path
- `log`: log level and format

Example multi-network layout:

```yaml
networks:
  - name: ouch
    server:
      host: irc.ouch.chat
      port: 6697
      tls: true
      verify_cert: true
    identity:
      nick: Echo
      user: Echo
      realname: I am a Go IRC Bot
      sasl_user: "Echo"
      sasl_pass: ""
    channels:
      - "#test123"
      - "#bots"

  - name: libera
    server:
      host: irc.libera.chat
      port: 6697
      tls: true
      verify_cert: true
    identity:
      nick: EchoLibera
      user: echobot
      realname: GoBot on Libera
      sasl_user: "EchoLibera"
      sasl_pass: ""
    channels:
      - "#example"
```

If `networks` is set, it takes precedence over the older single-network top-level fields.

## Plugins

GoBot ships with these plugins:

- `correction`: watches recent messages and supports IRC-style fixes like `s/wiiifee/wife`
- `banter`: optional conversational replies when the bot is directly addressed
- `urltitle`: fetches and posts page titles for shared URLs; YouTube links include the channel and video duration
- `weather`: current weather using Open-Meteo, no API key required
- `news`: headlines and search using NewsAPI
- `wikipedia`: article summaries from Wikipedia
- `seen`: records when a nick last spoke
- `tell`: queues a message for delivery when a nick speaks again
- `karma`: tracks `thing++` and `thing--`, and answers `!karma <thing>`
- `dice`: dice rolling commands
- `blackjack`: play a private per-user game of blackjack/21
- `poll`: create and vote in channel polls
- `remind`: schedule a reminder message
- `quote`: request a random quote from the configured quote sources
- `choose`: randomly choose between several options
- `time`: show the current time in an IANA timezone
- `channelstats`: in-memory per-channel message and user statistics
- `help`: lists commands and usage

Plugin toggles live under `plugins.<name>.enabled`.

`!help` shows a compact one-line plugin index. Use `!help <plugin>` for detailed usage, such as `!help poll`, `!help blackjack`, or `!help urltitle`.

### Blackjack / 21

Start a game with:

```text
!21
```

Then use:

- `!21 hit` to draw another card
- `!21 stand` to let the dealer finish
- `!21 double` to draw one final card and stand; this is available only on the initial two-card hand

Shortcut aliases are also available during a game: `!hit`, `!stand`, and `!double`. `!bj` is a short alias for `!21`.

### Polls

Create a poll with two or more options:

```text
!poll create Pizza or tacos? | Pizza | Tacos
!poll vote 1
!poll results
!poll close
```

Each nickname gets one vote, and voting again changes that nickname's vote. Polls are held in memory and reset when the bot restarts.

### Reminders, quotes, choices, time, and channel statistics

```text
!remind 30m check the logs
!quote
!choose pizza | tacos | burgers
!time America/Los_Angeles
!tz UTC
!stats
!chanstats
!wiki <query>
!wikipedia <query>
```

Reminders accept Go-style durations from `1s` through `720h` and are limited to 20 pending reminders per user/channel. They are in-memory and disappear after a restart. `!quote` uses the same built-in, configured, and fortune sources as banter. Channel statistics are runtime-only and report message totals, distinct users, and the top five users in the current channel.

Games are tracked separately for each nickname in each channel and are held in memory, so active games disappear if the bot restarts. Replies are posted to the channel. The dealer stands on 17. `!blackjack` is also accepted as an alias for `!21`.

## NickServ registration and SASL authentication

NickServ registration and SASL authentication are separate steps:

1. register the nickname/account on the IRC network
2. configure GoBot to log into that account automatically

First, connect with a normal IRC client and check the network’s NickServ help:

```text
/msg NickServ HELP REGISTER
```

On many networks the registration flow looks like:

```text
/msg NickServ REGISTER <strong-password> <email-address>
```

Then verify the account manually:

```text
/msg NickServ IDENTIFY <account-name> <strong-password>
```

After that, configure the bot with the registered nick and SASL account name.

Recommended split for a one-network deployment:

- keep `nick` and `sasl_user` in `config.yaml`
- keep the password in `.env`

Example `.env`:

```env
BOT_SASL_USER=Echo
BOT_SASL_PASS=replace-with-your-nickserv-password
BOT_NEWS_API_KEY=
BOT_STORAGE_DB_PATH=./data/bot.db
BOT_STATS_LISTEN_ADDRESS=127.0.0.1:8082
```

Protect it:

```sh
chmod 600 .env
```

Notes:

- `BOT_SASL_USER` and `BOT_SASL_PASS` override config values
- `BOT_SASL_PASS` authenticates the bot, it does not register the nickname
- do not commit NickServ passwords
- keep `verify_cert: true`

If a network sometimes renames the bot to `Guest...`, review these optional identity settings:

- `nickserv_fallback`
- `nickserv_ghost`

## Docker

The repository includes a multi-stage Dockerfile and a Compose file.

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

Compose behavior:

- mounts `config.yaml` read-only
- persists data in a named volume
- publishes the stats listener
- restarts the bot automatically unless stopped

Useful commands:

```sh
docker compose ps
curl http://localhost:8080/stats
docker compose down
```

`docker compose down -v` also removes the named volume and deletes stored bot data.

## First-run workflow

There is no setup wizard yet, so first run is configuration-first.

1. Build the binary with `make build` or `./scripts/build.sh`.
2. Review `config.yaml`.
3. Set at least one network, nick, and channel.
4. If using NickServ, create `.env` and set `BOT_SASL_USER` / `BOT_SASL_PASS`.
5. If using NewsAPI, set `BOT_NEWS_API_KEY`.
6. Start the bot.
7. Watch logs and confirm it joins the expected channels.
8. Test a few commands such as `!help`, `!weather`, `!wiki`, and `!karma`.

For direct binary use:

```sh
./bin/irc-bot
```

For Docker:

```sh
docker compose up -d
docker compose logs -f gobot
```

For config-only changes, restart the bot. You do not need to rebuild the Go binary unless the code changed.

## Optional banter plugin

The banter plugin adds occasional personality without turning the bot into constant channel noise. You can enable or disable it depending on how chatty you want the bot to be.

Key settings:

```yaml
plugins:
  banter:
    enabled: true
    probability: 0.25
    quotes_file: "quotes/banter.txt"
    fortune_dir: "/usr/share/games/fortunes"
```

Behavior:

- replies only when directly messaged or when its nick is mentioned in-channel
- ignores `!commands`
- picks a quote at random
- splits long quotes into safe IRC-sized chunks
- loads plain text quote files from `quotes/`
- optionally loads classic `fortune` quote files from `fortune_dir`

### Fortune integration

GoBot does not shell out to `fortune`. It reads quote files directly, which keeps behavior predictable and avoids command-execution surprises.

If you want system fortune databases on Debian/Ubuntu, install:

```sh
sudo apt update
sudo apt install fortune-mod fortunes-min
```

You can add more quote packs if your distro provides them, such as `fortunes`.

Common fortune directories include:

- `/usr/share/games/fortunes`
- `/usr/share/fortune`

If you only want the repository’s built-in banter file, you do not need any OS fortune packages. The included file is:

- `quotes/banter.txt`

You can also point `fortune_dir` at a cloned `fortune-mod` datfiles tree or any local directory containing classic `%`-delimited fortune text files.

## News plugin

The news plugin requires a NewsAPI key.

Enable it in config:

```yaml
plugins:
  news:
    enabled: true
    api_key: ""
    max_results: 3
```

Recommended secret handling:

- leave `api_key` empty in `config.yaml`
- set `BOT_NEWS_API_KEY` in `.env`

Usage:

- `!news` shows top US headlines
- `!news linux` searches recent English-language articles for `linux`

If no key is configured, the plugin responds with `news is not configured`.

## Stats and monitoring

GoBot exposes:

- `/stats`: human-readable JSON runtime stats for troubleshooting
- `/metrics`: Prometheus-compatible metrics for monitoring

Typical checks:

```sh
curl http://127.0.0.1:8082/stats
curl http://127.0.0.1:8082/metrics
```

Stats are in-memory and reset when the bot restarts.

### Scraping from a separate Prometheus host

If Prometheus runs on another machine, GoBot must listen on a VPS address reachable by that machine. For example, on the GoBot VPS set `.env` to the VPS's LAN address and port:

```env
BOT_STATS_LISTEN_ADDRESS=<BOT_VPS_LAN_IP>:8082
```

Find the correct VPS address with:

```sh
ip -br addr
```

Allow only the monitoring host through the VPS firewall. For UFW, where `192.168.70.107` is the Prometheus host:

```sh
sudo ufw allow from 192.0.2.50 to any port 8082 proto tcp
sudo systemctl restart gobot
```

Prometheus should scrape `/metrics`, not `/stats`:

```yaml
scrape_configs:
  - job_name: gobot
    metrics_path: /metrics
    static_configs:
      - targets:
          - <BOT_VPS_LAN_IP>:8082
```

Test connectivity from the Prometheus host:

```sh
curl http://<BOT_VPS_LAN_IP>:8082/metrics
```

Both endpoints have no built-in authentication. Restrict port `8082` to the Prometheus host and do not expose it to the public Internet.

## Security notes

- Keep `server.verify_cert: true` unless you have a controlled reason not to.
- Keep secrets in `.env` or your deployment secret store, not in Git.
- `/stats` and `/metrics` have no built-in authentication.
- Bind stats to localhost unless you intentionally expose them.
- The URL title plugin only fetches public HTTP/HTTPS targets and rejects loopback, private, link-local, multicast, and local hostnames to reduce SSRF risk.
- External HTTP lookups use timeouts.
- The Docker image runs as a non-root user.
- BoltDB data persists locally; protect filesystem access on the host.

## License

MIT
