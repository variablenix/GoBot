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
- foods: local food, cuisine, beer, coffee, tea, and wine suggestions
- sports: broad local sports suggestions
- car: broad local make, model, and production-span suggestions
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
- blackjack, pool, poll, remind, and duckhunt: games and activities

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

If the addressed nickname is GoBot's own configured nickname, it replies with
a short humorous message instead of queueing a message for itself. A queued
tell is delivered when that nickname next sends either a channel message or a
private message to GoBot.

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
compact IRC message and allows a fuller paragraph when it fits the configured
preview length. If the paragraph is longer, it is shortened and gets a
readable `Read more` link for the sign's daily horoscope page. The preview
length defaults to 360 characters and can be changed with
`plugins.horoscope.max_summary_length` (120–400 characters). For example, a
Virgo response links to the [Virgo daily horoscope
page](https://astrology.com.au/horoscopes/daily-horoscopes/virgo).

## Urban Dictionary

Look up a slang term. Add a result number for another definition:

~~~text
!urban yeet
!u yeet 2
!ud doomscrolling
~~~

GoBot returns one shortened definition and permalink. User-submitted text is
treated as untrusted: IRC control characters are stripped and output length
is bounded. The `1/10` notation means the first definition out of ten returned
by Urban Dictionary; it is not random. Add a number to select another result,
such as `!urban yeet 2`.

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
when you want Reddit-specific metadata for a particular post or one current
post from a subreddit:

~~~text
!reddit https://www.reddit.com/r/golang/comments/example/post-title/
!r https://www.reddit.com/r/golang/comments/example/post-title/
!reddit r/linux
!reddit https://www.reddit.com/r/linux/
!r r/golang
~~~

The response contains the post title, author, subreddit, score, comment count,
and canonical Reddit URL in one compact message. Both `r/subreddit` and full
Reddit subreddit URLs are accepted. GoBot prefers Reddit's public JSON
endpoint, then falls back to the corresponding public RSS feed when JSON is
temporarily blocked or throttled. RSS does not reliably include scores or
comment counts, so those fields are omitted rather than shown as false zeroes.
For individual posts, a final oEmbed title fallback can still provide the
title when Reddit rate-limits both metadata endpoints.
Only recognized Reddit hosts and paths are accepted; arbitrary URL fetching is
not performed by this command.

## Foods and drinks

Foods is a local, data-driven suggestion plugin. It does not call a food API
or send user input to a third-party service. Each request produces one short
IRC message:

~~~text
!food
!food ramen
!beer
!korean
!japanese
!sushi
!ramen
!chinese
!cantonese
!mandarin
!sichuan
!hunan
!shanghai
!beijing
!dongbei
!yunnan
!xinjiang
!burrito
!vietnamese
!filipino
!french
!spanish
!turkish
!middle-eastern
!german
!british
!irish
!polish
!portuguese
!greek
!soda
!juice
!water
!smoothie
!mocktail
!pizza
!tea nickname
~~~

The built-in lists include a broad `food` collection, beer styles from pale
ales and IPAs through lagers, stouts, sours, Belgian, farmhouse, and historical
styles, plus Korean, Japanese, sushi, ramen, Chinese, Indian, Thai, Mexican,
Italian, Mediterranean, American, pizza, taco, burrito, burger, pasta, dessert,
snack, coffee, tea, wine, Vietnamese, Filipino, French, Spanish, Turkish,
Ethiopian, Brazilian, Caribbean, Indonesian, Persian, and Middle Eastern
categories. European lists include German, British, Irish, Scottish, Welsh,
Portuguese, Greek, Polish, Ukrainian, Russian, Swedish, Norwegian, Danish,
Finnish, Dutch, Belgian, Austrian, Swiss, Czech, Hungarian, Romanian, and
Georgian dishes. The catalog also includes Moroccan, Nigerian, South African,
Peruvian, Argentinian, Chilean, Colombian, Venezuelan, Cuban, Canadian,
Australian, New Zealand, Malaysian, Pakistani, Bangladeshi, Sri Lankan, and
Nepalese dishes. Chinese regional lists include Cantonese, Northern Chinese
(available as `!mandarin`), Sichuan, Hunan, Shanghai, Beijing, Dongbei,
Yunnan, and Xinjiang dishes. `!foods` is an alias for `!food`; country and regional aliases
such as `!de`, `!uk`, `!ie`, `!pt`, `!gr`, `!pl`, `!ua`, `!ru`, `!se`, `!no`,
`!dk`, `!fi`, `!nl`, `!be`, `!at`, `!ch`, `!cz`, `!hu`, `!ro`, `!br`, `!pe`,
`!ar`, `!cl`, `!co`, `!ve`, and `!nz` are also available.

The cuisine lists are intentionally broad and easy to extend. They include
common dishes, regional specialties, street food, desserts, and familiar
variants such as `double cheeseburger`, `triple cheeseburger`, and several
burrito styles. Add one dish per line to a list in `data/foods/`; changes are
loaded when GoBot starts.

### Non-alcoholic drinks

Drink lists are local and include everyday beverages as well as international
styles. Use `!drinks` for a general suggestion or a focused command such as
`!soda`, `!juice`, `!water`, `!smoothie`, `!milkshake`, `!lemonade`,
`!mocktail`, `!energy-drink`, `!sports-drink`, `!hot-chocolate`, `!kombucha`,
or `!bubble-tea`. Coffee and tea remain available through `!coffee` and
`!tea`, while alcoholic suggestions remain under `!beer` and `!wine`.

Each drink list is one item per line under `data/foods/`, and the output is
bounded to one IRC message just like the cuisine lists.

## Sports

`!sports` returns one random sport from the local catalog. `!sport` is a short
alias, and an optional nickname makes the response more personal:

~~~text
!sports
!sport Alex
~~~

The catalog covers team sports, individual and racket sports, combat sports,
water and winter sports, motorsports, strength sports, adaptive sports, and
esports. Add one sport per line to `data/sports.txt`; the file is loaded when
GoBot starts. Every response is bounded to one IRC message.

## Cars

`!car` returns one random entry from the local car catalog; `!cars` is an
alias. Entries cover affordable daily drivers, trucks, SUVs, EVs, luxury cars,
classics, and sports and supercars. Each entry includes an approximate
production span:

~~~text
!car
!cars Alex
~~~

The catalog is intentionally local and easy to extend rather than pretending
to be a perfect worldwide vehicle registry. Add one make/model entry per line
to `data/cars.txt`; entries can include a year range such as
`Toyota Corolla (1966-present)`. The optional configuration is:

~~~yaml
plugins:
  car:
    enabled: true
    data_file: "data/cars.txt"
    max_length: 240
~~~

Lists are plain text, one item per line, under `data/foods/`. Operators can
extend or replace them without changing Go code:

~~~yaml
plugins:
  foods:
    enabled: true
    data_dir: "data/foods"
    max_length: 240
~~~

An optional nickname after a category produces a single targeted suggestion,
such as `Tea pick for Sam: genmaicha`. The plugin is bounded to one response
and local list lookups, so it does not create an external-request or flooding
hotspot.

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
!time Seoul
!time Bangkok
!car
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
- thing++ and thing-- also work as standalone ordinary chat messages. GoBot
  confirms each update in one compact line, such as `karma updated: thing=+2`;
  command messages are not treated as karma changes.
- dice accepts NdN notation or a single number such as !roll 20, meaning 1d20.
  It allows up to 100 dice and 10,000 sides per die and uses secure random
  values.
- quote reads the configured quote sources; see
  [Banter and fortune](banter-fortune.md).
- choose randomly selects between two to twenty pipe- or comma-separated
  options.
- time accepts full IANA timezone names and common city shortcuts such as
  `Seoul`, `Bangkok`, `Tokyo`, `London`, and `New York`; full names such as
  `Asia/Seoul` remain supported. Weather already accepts city names through
  Open-Meteo geocoding.
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
