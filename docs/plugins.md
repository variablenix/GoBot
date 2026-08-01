# Plugins and commands

Plugins are enabled or disabled under plugins.<name>.enabled in config.yaml.
!help prints a compact one-line index; use !help <plugin> for details.
!alias prints aliases without flooding the channel.

## Plugin index

- correction: IRC-style spelling corrections
- banter: optional replies when GoBot is directly addressed
- urltitle: page titles and YouTube metadata
- weather: Open-Meteo weather, no key required
- news: NewsAPI headlines and search
- wikipedia: English Wikipedia summaries
- horoscope: daily zodiac horoscopes
- urban: Urban Dictionary definitions
- reddit: compact Reddit post metadata
- define: short English dictionary definitions
- calc: safe local arithmetic and unit conversion
- status: local connection, uptime, and counter status
- xkcd: latest or numbered XKCD comics
- lastfm: current or recently played Last.fm tracks
- cats: short cat facts
- eightball: customizable Magic 8-Ball answers
- cheer: family-friendly cheers
- seen, tell, karma, and dice: channel utilities
- quote, choose, and time: lightweight utilities
- channelstats: persistent per-channel statistics
- help and alias: command discovery
- blackjack, poll, remind, and duckhunt: games and activities

Most command responses are rate-limited. See
[Configuration](configuration.md#rate-limits-and-join-warmup).

## Talking to GoBot

With banter enabled, address the bot in a channel:

~~~text
GoBot hello, how are you?
@GoBot hello
~~~

Banter is intentionally random and will not respond to every message. Commands
can be sent in a channel or by direct message where supported:

~~~text
!help
!weather Seattle
!quote
!tell username hello
~~~

!tell queues a message for the next time the addressed nickname speaks:

~~~text
!tell GoBot you are awesome
~~~

GoBot confirms that it will tell GoBot the next time they speak. When GoBot
next sends a message, the pending message is delivered in that channel or
conversation. Pending messages are stored in BoltDB and survive restarts.

## Correction

Use familiar IRC correction syntax immediately after the message to correct:

~~~text
I need a wiiifee
s/wiiifee/wife/
~~~

Add /g for every matching occurrence:

~~~text
s/wiiifee/wife/g
~~~

The correction history is kept in memory and controlled by history_size.

## URL titles

When someone posts an HTTP or HTTPS URL, GoBot fetches a title. It prefers Open
Graph and Twitter metadata, then falls back to the HTML title element.
YouTube links are formatted with title, channel, and duration when available:

~~~text
[YouTube] Video title | Channel: Example Channel | 1m 37s
~~~

An optional YouTube Data API v3 key gives the most reliable title, channel, and
duration data. Without it, GoBot uses public oEmbed, player metadata, and HTML
fallbacks; restricted or dynamically rendered videos may not expose duration.
Sites that block automated requests may not provide a title. Access-denied and
error titles are suppressed rather than posted.

Keep the key out of Git:

~~~env
BOT_YOUTUBE_API_KEY=your-youtube-data-api-key
~~~

The key needs YouTube Data API v3 enabled. GoBot requests only snippet and
contentDetails for the linked video.

## Weather

Weather uses Open-Meteo and does not require an API key:

~~~text
!weather Seattle
!wx Seattle
!forecast Seattle
!temp Seattle
~~~

Set default_units to metric or imperial.

## Horoscope

Fetch today's horoscope by zodiac sign. The sign is saved for your nickname:

~~~text
!horoscope aries
!horoscope
!zodiac
~~~

The public daily horoscope API requires no key. GoBot keeps the response to one
compact IRC message: it posts a short preview followed by a readable `Read
more` link for the sign's daily horoscope page. The preview length defaults to
220 characters and can be changed with `plugins.horoscope.max_summary_length`
(120–260 characters). For example, a Virgo response links to the [Virgo daily
horoscope page](https://astrology.com.au/horoscopes/daily-horoscopes/virgo).

## Urban Dictionary

Look up a slang term. Add a result number for another definition:

~~~text
!urban yeet
!u yeet 2
!ud doomscrolling
~~~

GoBot returns one shortened definition and permalink. User-submitted text is
treated as untrusted: IRC control characters are stripped and output length
is bounded.

## XKCD

~~~text
!xkcd
!xkcd latest
!xkcd 353
~~~

GoBot uses XKCD's official JSON interface and returns the number, title, date,
and link. The interface supports latest and numbered comics, not full-text
search.

## Last.fm

Last.fm requires a free API key:

~~~yaml
plugins:
  lastfm:
    enabled: true
    api_key: ""
~~~

Use a Last.fm username once; GoBot remembers it for your nickname:

~~~text
!lastfm username
!lastfm
!np
~~~

The response reports the current track or the most recently played track.
GoBot does not collect passwords or OAuth credentials.

## Cats

~~~text
!cats
!cat
~~~

The Cats plugin uses the public catfact.ninja endpoint, requires no key,
returns one cleaned and length-limited message, and does not download images.

## Status and uptime

These commands use only GoBot's local state and do not call an external
service:

~~~text
!status
!uptime
!ping
~~~

The response includes connection state, process uptime, configured network, and
message/command counters. It is useful for a quick IRC-side health check.

## Definitions

Look up one English word using the public Free Dictionary API:

~~~text
!define resilient
!def resilient
!dictionary resilient
~~~

GoBot returns the first concise definition and part of speech in one bounded
message. No API key is required. The lookup is English-only and third-party
text is cleaned before it is sent to IRC.

## Calculator and unit conversion

Calculator expressions are evaluated locally with a small parser; GoBot never
executes shell commands or arbitrary code:

~~~text
!calc 2*(3+4)
!math 100 / 4
!convert 10 km to mi
!convert 32 f to c
~~~

Supported conversions include temperature, length, mass, and volume. Keep
expressions short and use `+`, `-`, `*`, `/`, and parentheses.

## Reddit lookup

Posting a Reddit URL still uses the automatic URL-title plugin. Use `!reddit`
when you want Reddit-specific metadata for a particular post:

~~~text
!reddit https://www.reddit.com/r/golang/comments/example/post-title/
!r https://www.reddit.com/r/golang/comments/example/post-title/
~~~

The response contains the post title, author, subreddit, score, comment count,
and canonical Reddit URL in one compact message. Only recognized Reddit post
URLs are accepted; subreddit browsing and arbitrary URL fetching are not
performed by this command.

## Magic 8-Ball

~~~text
!8ball Will this deploy cleanly?
!8 Should I order pizza?
!eightball Is the channel ready?
~~~

Responses come from quotes/eightball.txt, one per line. Prefix a response with
green|, yellow|, or red| to control its IRC color. Unprefixed responses remain
readable without color. Each request produces one bounded message.

## Cheers

~~~text
!cheer
!yay
~~~

GoBot also responds to a message containing \o/, with a per-channel cooldown:

~~~yaml
plugins:
  cheer:
    enabled: true
    cheers_file: "quotes/cheers.txt"
    automatic_cooldown_seconds: 15
~~~

Responses come from quotes/cheers.txt, one family-friendly response per line.

## Wikipedia

Search English Wikipedia with either alias:

~~~text
!wiki Linux
!wikipedia FreeBSD
~~~

GoBot tries an exact article title, then searches Wikipedia if needed.
max_summary_length controls response length.

## Channel utilities

~~~text
!seen nickname
!tell nickname message for them
!karma project
project++
project--
!roll d20
!roll 2d6
!quote
!choose pizza | tacos | burgers
!time America/Los_Angeles
!stats
!chanstats
!channelstats
!status
!define resilient
!calc 2+2
!convert 10 km to mi
!reddit https://www.reddit.com/r/example/comments/abc123/post/
~~~

- seen reports where and when a nickname last spoke. Records are stored in
  BoltDB.
- tell queues a message and delivers it when the addressed nickname next speaks.
- karma tracks case-insensitive thing++ and thing-- changes.
- dice accepts NdN notation or a single number such as !roll 20, meaning 1d20.
  It allows up to 100 dice and 10,000 sides per die and uses secure random
  values.
- quote reads the configured quote sources; see
  [Banter and fortune](banter-fortune.md).
- choose randomly selects between two to twenty pipe- or comma-separated
  options.
- time uses IANA timezone names, with common aliases such as EST.
- channelstats reports message totals, distinct users, and the top five users
  for the current channel. It is persisted in BoltDB.

## Help and aliases

~~~text
!help
!help weather
!help blackjack
!alias
!alias wiki
~~~

!help shows the compact plugin index. !help <plugin> gives detailed usage, and
!alias [command] lists aliases without scanning a long response.
