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

Persistent plugin state such as seen/karma data is stored in BoltDB. Runtime counters are exposed on `/stats` and `/metrics` and their cumulative totals survive restarts.

## Requirements

- Go `1.25+`
- A writable filesystem for the BoltDB database
- IRC network access
- Optional: Docker / Docker Compose
- Optional: fortune packages if you want a curated set of extra banter quote sources

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
- `owner_accounts`: authenticated IRC account names reserved for future owner-only features
- `invites`: whether the bot accepts IRC invitations and the invite cooldown
- `rate_limit`: outbound message pacing, per-user command cooldowns, and cooldown-warning pacing
- `plugins`: plugin enable/disable and plugin-specific settings
- `stats`: HTTP stats listener
- `storage`: BoltDB path
- `log`: log level and format

GoBot ignores channel messages for 10 seconds after joining by default. This prevents IRC history replay or relay backlog from triggering URL previews, banter, and other plugins as a flood. Adjust the window with `rate_limit.join_warmup_seconds`; set it to `0` to use the 10-second default.

Example multi-network layout:

```yaml
networks:
  - name: primary
    server:
      host: irc.example.net
      port: 6697
      tls: true
      verify_cert: true
    identity:
      nick: GoBot
      user: gobot
      realname: Go IRC Bot
      sasl_user: ""
      sasl_pass: ""
    channels:
      - "#example"
      - "#bots"

  - name: secondary
    server:
      host: irc.example.org
      port: 6697
      tls: true
      verify_cert: true
    identity:
      nick: GoBot2
      user: gobot2
      realname: GoBot on secondary network
      sasl_user: ""
      sasl_pass: ""
    channels:
      - "#example"
```

If `networks` is set, it takes precedence over the older single-network top-level fields.

### Owners and channel invitations

Owners are configured out-of-band using authenticated NickServ/IRC accounts. Nicknames alone are never treated as proof of ownership:

```yaml
owner_accounts:
  - "your-account"
  - "another-owner"
```

GoBot records the IRCv3 `account` tag when the network provides it and exposes owner status to future owner-only features. There is intentionally no ownership-claim command, so nobody can claim ownership from IRC.

Anyone may invite the bot to a valid channel when invitations are enabled:

```yaml
invites:
  enabled: true
  cooldown_seconds: 30
```

From an IRC client:

```text
/invite Echo #new-channel
```

GoBot joins the invited channel after validating the channel name. Invitations are rate-limited globally per bot/network and per channel. Invited channels are temporary and are not written to `config.yaml`; add a channel to the network's `channels` list if it should be rejoined after a restart.

All command responses are rate-limited per authenticated account or sender identity. This includes `!help`, `!weather`, `!roll`, `!poll`, and every other command. Configure it with:

```yaml
rate_limit:
  command_cooldown_seconds: 2
  command_warning_cooldown_seconds: 10
```

When a user sends commands too quickly, GoBot replies once with `command cooldown—please wait a moment`. Warning messages have their own cooldown, so repeated violations cannot flood the channel. The normal outbound queue rate limit still applies to all bot messages.

### IRC formatting

Selected command responses use standard mIRC IRC color and bold control codes for headings, success, warning, and error states. These are not terminal-specific ANSI escapes: clients such as WeeChat, Irssi, and Relay can render them, while clients without formatting support show the same underlying text. Blackjack also uses UTF-8 suit symbols; clients with limited Unicode fonts may display those symbols as replacement characters.

## Plugins

GoBot ships with these plugins:

- `correction`: watches recent messages and supports IRC-style fixes like `s/wiiifee/wife`
- `banter`: optional conversational replies when the bot is directly addressed
- `urltitle`: fetches and posts page titles for shared URLs; YouTube links include the channel and video duration
- `weather`: current weather using Open-Meteo, no API key required; aliases: `!wx`, `!forecast`, and `!temp`
- `news`: headlines and search using NewsAPI
- `wikipedia`: article summaries from Wikipedia
- `seen`: records when a nick last spoke
- `tell`: immediately relays a message to the current channel or conversation
- `karma`: tracks `thing++` and `thing--`, and answers `!karma <thing>`
- `dice`: dice rolling commands
- `blackjack`: play a separate per-user game of blackjack/21
- `poll`: create and vote in channel polls
- `remind`: schedule a reminder message
- `quote`: request a random quote from the configured quote sources
- `choose`: randomly choose between several options
- `time`: show the current time in an IANA timezone
- `channelstats`: persistent per-channel message and user statistics
- `help`: lists commands and usage
- `alias`: lists command aliases and supports per-command alias details

Plugin toggles live under `plugins.<name>.enabled`.

`!help` shows a compact one-line plugin index. Use `!help <plugin>` for detailed usage, such as `!help poll`, `!help blackjack`, or `!help urltitle`.
Use `!alias` for a compact grouped alias list, or `!alias wiki` to inspect one command's aliases.

### Talking to GoBot

With the optional banter plugin enabled, address the bot in a channel:

```text
Echo hello, how are you?
@Echo hello
```

Banter replies are intentionally random and may not respond to every message. Commands can be sent in a channel or by direct message where the command supports it:

```text
!help
!weather Seattle
!wx Seattle
!quote
!tell username hello
```

`!tell` immediately relays a message to the current channel or conversation:

```text
!tell Echo you are awesome
```

GoBot posts `Echo: you are awesome` immediately. `owner_accounts` is reserved for future owner-only features.

### Correction

Use the familiar IRC correction syntax immediately after your message:

```text
I need a wiiifee
s/wiiifee/wife
```

GoBot corrects the most recent matching message from that nickname. Add `/g` for every matching occurrence:

```text
s/wiiifee/wife/g
```

The correction history is kept in memory and controlled by `history_size`.

### URL titles

When someone posts an HTTP or HTTPS URL in a channel, `urltitle` fetches a page title and posts it. It prefers Open Graph and Twitter metadata, then falls back to the HTML `<title>` element. YouTube links are formatted with the title, channel, and duration:

```text
[YouTube] Video title | Channel: Example Channel | 1m 37s
```

For the most reliable YouTube duration data, configure an optional YouTube Data API v3 key. Without it, GoBot uses public oEmbed, player metadata, and HTML fallbacks; some restricted or dynamically rendered videos may not expose their duration. Sites that block automated requests may not provide a title; GoBot suppresses access-denied/error titles rather than posting them.

Keep the key out of Git and set it in `.env`:

```text
BOT_YOUTUBE_API_KEY=your-youtube-data-api-key
```

The API key must have YouTube Data API v3 enabled. GoBot requests only `snippet` and `contentDetails` for the linked video; the API response supplies the canonical title, channel, and ISO-8601 duration.

### Weather

Weather uses Open-Meteo and does not require an API key:

```text
!weather Seattle
```

Set `default_units` to `metric` or `imperial` in `config.yaml`.

### Wikipedia

Use either command alias to search English Wikipedia and receive a summary:

```text
!wiki Linux
!wikipedia FreeBSD
```

GoBot first tries the article title, then searches Wikipedia if the query is not an exact title. `max_summary_length` controls the response length.

### Seen, tell, karma, and dice

```text
!seen nickname
!tell nickname message for them
!karma project
project++
project--
!roll d20
!roll 2d6
```

- `seen` reports the channel and message from the last time a nickname spoke. Its records are stored in BoltDB.
- `tell` immediately relays the requested message in the current channel or conversation.
- `karma` tracks case-insensitive `thing++` and `thing--` changes and reports totals with `!karma <thing>`.
- `dice` accepts `NdN` notation or a single number such as `!roll 20`, which means `1d20`. It allows up to 100 dice and 10,000 sides per die and uses secure random values.

### Help

`!help` returns a compact plugin index. Ask for details without flooding the channel:

```text
!help
!help weather
!help urltitle
!help blackjack
```

List aliases without scanning the full help output:

```text
!alias
!alias blackjack
```

### Blackjack / 21

Start a game with:

```text
!21
```

Then use:

- `!21 hit` to draw another card
- `!21 stand` to let the dealer finish
- `!21 double` to draw one final card and stand; this is available only on the initial two-card hand

View your persistent personal record or the top five players:

```text
!bj stats
!bj leaderboard
```

Shortcut aliases are also available during a game: `!hit`, `!stand`, and `!double`. `!bj` is a short alias for `!21`.

Games are tracked separately for each nickname in each channel and are held in memory, so active games disappear if the bot restarts. Abandoned games expire after 30 minutes. Replies are posted to the channel. The dealer stands on 17. `!blackjack` is also accepted as an alias for `!21`.

Blackjack records are stored in BoltDB and survive restarts. GoBot tracks hands, wins, losses, pushes, blackjacks, busts, win rate, and streaks. Authenticated IRC account names are preferred for identity; nicknames are used when no account tag is available. No real-money or virtual-currency betting is involved.

Blackjack replies use standard IRC color formatting where supported, plus UTF-8 suit symbols such as `♠`, `♥`, `♦`, and `♣`. Clients without color or Unicode support still receive readable card ranks and game results, although the suit symbols may display differently.

### Polls

Create a poll with two or more options:

```text
!poll create Pizza or tacos? | Pizza | Tacos
!poll vote 1
!poll results
!poll close
```

Each nickname gets one vote, and voting again changes that nickname's vote. Active polls are stored in BoltDB and restored after a restart. Polls automatically expire after 15 days.

### Reminders

```text
!remind 30m check the logs
!remind 45s check the connection
!remind 2h restart the service
!remind 1h30m check the deployment
!remind 72h check in three days
```

Reminders accept Go-style durations from `1s` through `360h` and are limited to 20 pending reminders per user/channel. Supported units are `s` (seconds), `m` (minutes), and `h` (hours). Days must be expressed as hours because `d` is not a valid Go duration; use `72h` for three days. Pending reminders are stored in BoltDB and rescheduled after a restart; reminders older than 15 days are discarded.

### Quotes

```text
!quote
```

The quote plugin uses the same built-in, configured, and fortune sources as banter. It reads files directly and never executes the `fortune` command.

### Random choices

Choose between two to twenty options using pipes or commas:

```text
!choose pizza | tacos | burgers
!choose red, blue, green
```

### Timezones

Use IANA timezone names, with a few common aliases:

```text
!time America/Los_Angeles
!tz UTC
!time EST
```

### Channel statistics

Use any of these aliases:

```text
!stats
!chanstats
!channelstats
```

The response reports message totals, distinct users, and the top five users for the current channel. HTTP `/stats`, `/metrics`, and `!stats` channel details persist in BoltDB and survive restarts.

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
curl http://localhost:8082/stats
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
9. To add a temporary channel, invite the bot with `/invite <bot-nick> #channel`; add permanent channels to `config.yaml`.

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

The recommended setup is to install the fortune data packages, then copy only the collections you want into a separate curated directory. This avoids loading every system fortune into the bot and makes the bot's quote sources explicit.

On Debian or Ubuntu, the following installs the data and creates a curated set containing `computers`, `linux`, `science`, `tao`, and `wisdom` in one step:

```sh
sudo apt update && sudo apt install -y fortune-mod fortunes-min fortunes && \
mkdir -p "$HOME/irc-bot/fortune-curated" && \
for quote in computers linux science tao wisdom; do \
  source_file="$(find /usr/share/games/fortunes /usr/share/fortune -maxdepth 1 -type f -name "$quote" -print -quit 2>/dev/null)"; \
  if [ -n "$source_file" ]; then \
    cp "$source_file" "$HOME/irc-bot/fortune-curated/$quote"; \
  else \
    printf 'Warning: fortune collection not found: %s\n' "$quote" >&2; \
  fi; \
done
```

The package names and source directories can vary by distribution. If a collection is not found, locate it with:

```sh
find /usr/share/games/fortunes /usr/share/fortune -maxdepth 1 -type f 2>/dev/null | sort
```

Add or remove names in the loop to choose a different curated set. The files are classic `%`-delimited fortune files; GoBot parses those delimiters directly.

Point `fortune_dir` at the resulting directory using an absolute path:

```yaml
plugins:
  banter:
    enabled: true
    probability: 0.25
    quotes_file: "quotes/banter.txt"
    fortune_dir: "/path/to/your/fortune-curated"
  quote:
    enabled: true
    fortune_dir: "/path/to/your/fortune-curated"
```

Replace `/path/to/your/fortune-curated` with the real path on the host, for example the `$HOME/irc-bot/fortune-curated` directory created above. Configuration values are read by GoBot as written, so use the expanded absolute path rather than relying on `$HOME` inside YAML.

Both banter and `!quote` use the configured fortune directory. Every regular file in that directory is loaded, so the curated directory can contain exactly the collections you selected. Files ending in `.dat` or `.u8` are ignored. The repository's built-in file is also loaded automatically:

- `quotes/banter.txt`

You do not need OS fortune packages if you only want `quotes/banter.txt`. You may also point `fortune_dir` at another local directory containing classic `%`-delimited fortune text files, but avoid pointing it at a large system-wide collection unless you intentionally want all of it loaded.

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

The cumulative counters persist in BoltDB and survive restarts. Uptime and current connection status are runtime values and naturally reset or change when the process restarts.

### Scraping from a separate Prometheus host

If Prometheus runs on another machine, GoBot must listen on a VPS address reachable by that machine. For example, on the GoBot VPS set `.env` to the VPS's LAN address and port:

```env
BOT_STATS_LISTEN_ADDRESS=<BOT_VPS_LAN_IP>:8082
```

Find the correct VPS address with:

```sh
ip -br addr
```

Allow only the monitoring host through the VPS firewall. For UFW, where `192.0.2.50` is the Prometheus host:

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
- GoBot refuses to send SASL or NickServ credentials over a non-TLS IRC connection.
- Keep secrets in `.env` or your deployment secret store, not in Git.
- Configure owner accounts by authenticated account name; do not rely on nicknames for authorization.
- `/stats` and `/metrics` have no built-in authentication.
- Bind stats to localhost unless you intentionally expose them.
- The URL title plugin only fetches public HTTP/HTTPS targets and rejects loopback, private, link-local, multicast, and local hostnames to reduce SSRF risk.
- External HTTP lookups use timeouts.
- IRC invitations, command handling, and cooldown warnings are rate-limited to reduce channel and join flooding.
- The Docker image runs as a non-root user.
- BoltDB data persists locally; protect filesystem access on the host.

## License

MIT
