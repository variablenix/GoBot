# Configuration

GoBot reads `config.yaml` from its working directory. Use one entry under
`networks` for each IRC network and list any number of channels under that
network.

## Multi-network configuration

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
      - "#quiet"
      - "#lobby"
    # Optional per-channel opt-outs. Unlisted plugins keep their global setting.
    # Only #quiet gets these overrides; #lobby keeps the global plugin settings.
    plugin_overrides:
      "#quiet":
        banter: false
        urltitle: false
        duckhunt: false

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

`networks` takes precedence over the older single-network top-level fields.
Each network has its own IRC connection, identity, channels, SASL settings,
and plugin activity. To reduce chatter in one channel without changing the
global plugin configuration, add `plugin_overrides` under that network. The
map key is the channel name and each nested key is a canonical plugin name.
Set a plugin to `false` to disable it in that channel; omitted plugins remain
enabled. Channel and plugin names are matched case-insensitively. Overrides
also keep disabled plugins out of `!help` and `!alias`, and event-driven
plugins such as Duck Hunt are stopped cleanly for that channel.

For example, with the configuration above, `#quiet` disables banter, URL title
lookups, and Duck Hunt, while `#lobby` continues using the global settings. To
disable a different plugin in `#lobby`, add another entry without changing the
`#quiet` entry:

```yaml
plugin_overrides:
  "#quiet":
    banter: false
    urltitle: false
    duckhunt: false
  "#lobby":
    banter: false
```

## Main settings

- `command_prefix`: command prefix, normally `!`
- `owner_accounts`: authenticated IRC account names reserved for owner-only
  controls
- `invites`: whether the bot accepts channel invitations and the invite
  cooldown
- `rate_limit`: outbound pacing, command cooldowns, warning pacing, and join
  warmup
- `plugins`: plugin enable/disable flags and plugin-specific settings
- `stats`: HTTP listener for `/stats` and `/metrics`
- `storage.db_path`: BoltDB data path
- `log`: log level and output format

The example configuration includes these optional utility plugins:

```yaml
plugins:
  status: {enabled: true}
  define: {enabled: true, timeout_seconds: 8, max_length: 240}
  calc: {enabled: true}
  fun: {enabled: true, data_dir: "data/fun", max_length: 240}
  weapons: {enabled: true, data_file: "data/weapons.txt", max_length: 240}
  github: {enabled: true, timeout_seconds: 8, max_length: 360, token: ""}
  reddit: {enabled: true, timeout_seconds: 8, max_length: 360}
  daily: {enabled: true, bonus_xp: 25}
  # Keyless source-grounded answers by default; AI rewriting is opt-in.
  ask:
    enabled: true
    wikipedia_first: true
    duckduckgo_fallback: true
    ai_rewrite: false
    provider: none # none, openrouter, openai, gemini, ollama
    max_length: 360
    max_response_chars: 240
    timeout_seconds: 12
    cooldown_seconds: 15
    openrouter_model: google/gemma-4-31b-it:free
    openai_model: gpt-4o-mini
    gemini_model: gemini-2.0-flash
    ollama_model: llama3.2
    ollama_url: http://127.0.0.1:11434
  grab: {enabled: true, max_length: 320, max_quotes_per_user: 20}
  linux: {enabled: true, timeout_seconds: 8, max_length: 260}
  foods: {enabled: true, data_dir: "data/foods", max_length: 240}
  sports: {enabled: true, data_file: "data/sports.txt", max_length: 200}
  car: {enabled: true, data_file: "data/cars.txt", max_length: 240}
  # Optional playful join greetings; disabled by default.
  welcome: {enabled: false, probability: 0.15, cooldown_seconds: 120, messages_file: "data/welcome.txt"}
  # 65% is a balanced casual default; use 100% for deterministic testing.
  pool: {enabled: true, game_timeout_minutes: 30, turn_timeout_seconds: 120, shot_success_percent: 65}
  horoscope: {enabled: true, max_summary_length: 360}
```

`status`, `calc`, `foods`, `sports`, `car`, and `weapons` are local. `ask` uses
English Wikipedia first and DuckDuckGo's public Instant Answer endpoint as a
fallback. In its default `provider: none` mode it needs no API key and returns
a compact source-grounded answer with a source link. Set `ai_rewrite: true`
and choose `openrouter`, `openai`, `gemini`, or `ollama` to optionally rewrite
the retrieved source into a more conversational answer. Provider credentials
belong in environment variables such as `BOT_OPENROUTER_API_KEY`,
`BOT_OPENAI_API_KEY`, or `BOT_GEMINI_API_KEY`; never commit them to this file.
`define` uses the public English dictionary service, `reddit` uses Reddit's public post and
subreddit JSON endpoints with an RSS fallback, and `horoscope` uses a public
daily horoscope API. `foods` and `sports` use local text files only. `fun` uses
local operator-editable text catalogs under `data/fun`, and `weapons` uses the
high-level reference catalog under `data/weapons.txt`; it does not provide
instructions or tactical guidance. `github` uses read-only public GitHub API
lookups. Their output limits
prevent a slow or unusually large response from holding up the bot or flooding
IRC. Disable any of them with `enabled: false` if they are not wanted.

`daily` provides `!daily` in channels. Each authenticated account can claim
once per UTC calendar day, regardless of channel or network; users without an
account tag are limited by network and nickname. Different users can each
claim the same day. The default reward is 25 XP, added to the Duck Hunt
progression profile for the channel where it is claimed. Daily bonuses require
Duck Hunt to be enabled globally and for that channel. Claims are persisted in
the configured database, so restarting GoBot does not reset the daily limit.

The optional `welcome` plugin listens for users joining a channel. It applies
the configured probability to each join and then enforces a per-channel
cooldown, so it can add personality without greeting every person in a busy
room. Lines are read from `data/welcome.txt`; `{nick}` is replaced with the
joining nickname. Keep each line short enough for one IRC message. The plugin
also respects `join_warmup_seconds`, so replayed join events during startup do
not cause a burst of greetings.

For deployment secrets, use `.env` or the service manager's environment rather
than committing them to the example configuration. `BOT_GITHUB_TOKEN` is
optional and is only needed when the anonymous GitHub API limit is not enough.

## Owners and invitations

Owners are configured out-of-band using authenticated IRC accounts. Nicknames
alone are never treated as proof of ownership:

```yaml
owner_accounts:
  - "your-account"
  - "another-owner"
```

GoBot records the IRCv3 `account` tag when the network provides it. There is
intentionally no ownership-claim command.

### Owner-only private reload

An owner can reload file-backed plugin settings without disconnecting GoBot:

```text
/msg GoBot reload
/msg GoBot !reload
```

The sender must be identified to an IRC account listed in `owner_accounts`; a
nickname alone is not accepted. GoBot replies privately after the reload and
keeps the existing IRC connection open.

The reload applies settings for active plugins that support runtime reloads,
including `ask`, refreshes the `plugin_overrides` for the network on which the
private message was received, and applies global plugin enable/disable changes.
For example, changing `plugins.choose.enabled` from `true` to `false` stops
`!choose` immediately; changing it back to `true` initializes it and enables it
again without disconnecting. The same applies to channel overrides such as
`duckhunt: false`. For a multi-network configuration, GoBot selects the
override map belonging to the connected network; legacy single-network
`plugin_overrides` is supported too.

The reload does not change the server, nickname, channel membership, or
`owner_accounts`. Those changes require a restart. Environment variables loaded
from `.env` are process settings as well, so changing `.env` requires a
restart.

Anyone may invite the bot when invitations are enabled:

```yaml
invites:
  enabled: true
  cooldown_seconds: 30
```

Use this from an IRC client:

```text
/invite GoBot #new-channel
```

The channel name is validated before GoBot joins. Invited channels are
temporary and are not written to `config.yaml`; add permanent channels to the
network's `channels` list.

## Rate limits and join warmup

All command responses are rate-limited per authenticated account or sender
identity, including help, games, weather, polls, and URL-triggered responses.

```yaml
rate_limit:
  messages_per_second: 1.5
  burst: 5
  command_cooldown_seconds: 2
  command_warning_cooldown_seconds: 10
  join_warmup_seconds: 30
```

If commands arrive too quickly, GoBot sends a short cooldown notice. Notices
have their own cooldown so they cannot become a second flood source. The
outbound queue limit applies to every bot message.

GoBot ignores channel messages during `join_warmup_seconds` after joining.
This protects against IRC history replay or relay backlog causing a burst of
URL previews, banter, and other automatic responses.

## IRC formatting

Selected responses use standard mIRC IRC color and bold control codes. These
are not terminal-specific ANSI escapes: WeeChat, Irssi, Relay, and compatible
clients can render them, while clients without formatting support show the
same underlying text.

Blackjack and Duck Hunt also use UTF-8 symbols. Clients with limited fonts may
display those symbols as replacement characters, but the surrounding text
remains readable.
