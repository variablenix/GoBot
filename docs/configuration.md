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

`networks` takes precedence over the older single-network top-level fields.
Each network has its own IRC connection, identity, channels, SASL settings,
and plugin activity.

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
  reddit: {enabled: true, timeout_seconds: 8, max_length: 360}
```

`status` and `calc` are local. `define` uses the public English dictionary
service, and `reddit` uses Reddit's public post JSON endpoint. Their timeout
and output limits prevent a slow or unusually large response from holding up
the bot or flooding IRC. Disable any of them with `enabled: false` if they are
not wanted.

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
