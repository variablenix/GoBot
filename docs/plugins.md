# Plugins and commands

Plugins are enabled or disabled under plugins.<name>.enabled in config.yaml.
!help prints a compact one-line index; use !help <plugin> for details.
!alias prints aliases without flooding the channel.

## Plugin index

- correction: IRC-style spelling corrections
- banter: optional replies when GoBot is directly addressed
- welcome: optional, probability-based join greetings with a per-channel cooldown
- urltitle: page titles and YouTube metadata
- youtube: single-result YouTube video and music search
- cve: NVD CVE lookup with CVSS and affected software
- ipinfo: IP/ASN context through IP-API
- acronym: local operator-maintained acronym expansion
- weather: Open-Meteo weather, no key required
- steam: Steam game search, genre links, and most-played lookup, no key required
- news: NewsAPI headlines and search
- ask: DuckDuckGo Search Assist with bounded public-result excerpts, Instant Answer, and Wikidata fallbacks
- wikipedia: English Wikipedia summaries
- horoscope: daily zodiac horoscopes
- urban: Urban Dictionary definitions
- reddit: compact Reddit post metadata
- foods: local food, cuisine, beer, coffee, tea, and wine suggestions
- sports: broad local sports suggestions
- car: broad local make, model, and production-span suggestions
- define: short English dictionary definitions
- calc: safe local arithmetic and unit conversion
- github: compact public GitHub repository, issue, release, user, commit, and search lookups
- grab: save, replay, search, and randomly show memorable channel lines
- note: private per-account notes with bounded persistence and expiry
- linux: current Linux kernel release lines from kernel.org
- weapons: high-level local firearm and weapons-name catalog
- paste: create bounded pastes through a configured Opengist instance
- crypto: local hashes, Base64, and URL encoding utilities
- pkg: Go, npm, and PyPI package metadata lookups
- port: local bidirectional well-known port/service lookup
- audit: package vulnerability discovery through OSV
- docker: Docker Hub image metadata lookup
- status: local connection, uptime, and counter status
- xkcd: latest or numbered XKCD comics
- lastfm: current or recently played Last.fm tracks
- cats: short cat facts
- eightball: customizable Magic 8-Ball answers
- doobie: 3-2-1 smoke countdown with `!doobie`, `!420`, `$doobie`, or `$420`
- fun: local yo-momma jokes, one-liners, puns, and wisdom
- attack: playful target-based actions and messages
- luv: persistent blue-heart points for spreading kindness
- scramble: local first-correct-answer word game
- puzzle: timed local numbers, trivia, word, logic, anagram, and crossword puzzles
- cheer: family-friendly cheers
- seen, tell, karma, and dice: channel utilities
- quote, choose, and time: lightweight utilities
- channelstats: persistent per-channel statistics
- help and alias: command discovery
- blackjack, pool, poll, remind, and duckhunt: games and activities

Most command responses are rate-limited. See
[Configuration](configuration.md#rate-limits-and-join-warmup).

Every command in the six plugins below emits exactly one bounded IRC line per
invocation. Third-party text is sanitized, and summaries use truncation or a
`+ N more` suffix rather than sending additional lines.

## Paste

`!paste <text>` creates a paste through an Opengist-compatible server. If the
argument is an HTTP or HTTPS URL, GoBot fetches a bounded amount of its content
before creating the paste. Configure the server and token out of band:

~~~yaml
plugins:
  paste:
    enabled: true
    provider: opengist
    base_url: "https://paste.example.net"
    default_visibility: unlisted
    max_input_length: 4096
~~~

Set `BOT_PASTE_BASE_URL` and `BOT_PASTE_TOKEN` in `.env` or the service
environment. The token is never read from `config.yaml`; oversized input is
truncated and reported in the single response line. URL fetching is an
outbound request made from the bot host, disables proxies, rejects private and
local targets, and follows only redirects that resolve to public HTTP(S)
addresses. Keep normal host egress controls in place because URL pasting is
still an outbound network capability. To insert real line breaks into inline IRC text, enable
`hard_wrap` and choose a `hard_wrap_width` (80 by default). URL-fetched content
is left unchanged unless `hard_wrap_urls: true` is also configured; softwrap in
the Opengist editor remains a display preference and does not change file
content.

## Crypto and encoding

The `crypto` plugin performs local-only operations with the Go standard
library:

~~~text
!hash sha256 hello
!md5 hello
!sha1 hello
!sha256 hello
!sha512 hello
!b64encode hello world
!b64decode aGVsbG8=
!urlencode hello world
!urldecode hello+world
~~~

Input is capped at 512 characters and results are sanitized before being sent
to IRC. Invalid input returns one error line. These commands make no network
requests. MD5 and SHA-1 are provided for compatibility and checksums only; do
not use them for password storage, signatures, or other security decisions.

## Package metadata

Use `!pkg go`, `!pkg npm`, or `!pkg pip` for current registry metadata, with an
optional version for a specific release. `!package` is an alias. The plugin
uses the public Go module proxy, npm registry, and PyPI endpoints, requires no
API keys, and bounds both request time and response length. The Go module proxy
response is handled using its canonical `Version` field, so paths such as
`gopkg.in/irc.v3` work correctly.

Examples:

~~~text
!pkg go github.com/variablenix/GoBot
!pkg npm lodash
!pkg pip requests 2.32.3
!pkg go irc
~~~

Responses include the registry version, a sanitized description when present,
and the canonical package page. If an exact name is not found, GoBot performs a
bounded best-effort search and returns possible Go, npm, or PyPI matches with
links; use the suggested full name for metadata. `!package` is the only alias.

## Ports

`!port 443` looks up a port number and `!port ssh` looks up a service name.
`!ports` is an alias. The catalog is local and can be maintained in
`data/ports.txt`; it does not make network requests. The catalog includes every
port from 0 through 1023 plus common higher-numbered services. Output is
bounded to one line and the plugin has no configurable `max_length`.

## Vulnerability audit

`!audit <go|npm|pip> <package> [version]` queries OSV for known package
vulnerabilities. `!vuln` and `!osv` are aliases. Without a version, GoBot also
checks the package's current registry version and reports whether that latest
version is affected. Set `max_vulns_shown` to control the number of CVEs shown
in the one-line summary; `timeout_seconds` and `max_length` also apply. No API
key is required. With no version, the request omits the OSV `version` field,
then fetches the latest registry version and evaluates OSV affected ranges.
With a version, it performs an exact OSV query. Severity comes from OSV's
database-specific or severity fields, and fixed versions are shown when OSV
provides them. If the package cannot be resolved exactly, GoBot returns a
bounded list of possible Go, npm, or PyPI package names; it does not audit all
fuzzy matches automatically.

## Docker Hub

`!docker nginx` looks up an official Docker Hub image, while
`!docker traefik/traefik` looks up a user or organization image. `!hub` and
`!dockerhub` are aliases. The plugin uses Docker Hub's public repository and
tag APIs, formats pull counts compactly, and keeps the response to one IRC
line. An image without a slash uses Docker Hub's `library` namespace and links
to `hub.docker.com/_/<image>`; an image containing one slash is treated as a
user or organization image and links to `hub.docker.com/r/<user>/<image>`.

The full `!help` menu is kept short in channels. If it would exceed one
message, GoBot sends the complete menu to the requesting user's PM and posts a
brief notice in the channel. Plugin-specific help uses the same behavior.

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

## Personal notes

The `note` plugin stores small, user-invoked notes by authenticated IRC account:

~~~text
!note add vegas https://example.com
!note vegas
!note list
!note delete vegas
!note clear
~~~

`!notes` is an alias for `!note`. Notes are not announced automatically and are
stored separately for each user. A note response sent in a channel is visible
there, so use a private message when the note is sensitive. When account tags
are unavailable, GoBot falls back to the network and nickname. Notes are limited
to 50 per user by default, with 400 characters per note, and unused notes expire
after 180 days. Operators can change these limits with `plugins.note.max_notes`,
`plugins.note.max_note_length`, and `plugins.note.expiry_days`; set
`expiry_days: 0` to disable expiry.

## Join greetings

The optional `welcome` plugin posts a short, playful line when another user
joins a channel. It is disabled by default. Enable it in `config.yaml`:

~~~yaml
plugins:
  welcome:
    enabled: true
    probability: 0.15
    cooldown_seconds: 120
    messages_file: "data/welcome.txt"
~~~

`probability` is the chance that an individual join produces a greeting. The
cooldown is tracked separately for each channel, so a busy channel cannot be
flooded by a greeting for every arrival. The catalog contains short original
lines and supports the `{nick}` placeholder:

~~~text
The Evil Has Landed.
A wild Pzycho appeared!
~~~

There is no command to enable it at runtime. Once enabled, it applies to
channels where GoBot is present. The bot's own join is ignored, and the normal
post-join warmup also suppresses replayed events during startup.

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

## YouTube search

Search YouTube for one regular video or music video and receive a concise
short link:

~~~text
!yt SMOKE WEED EVERYDAY
!youtube Linux server setup
~~~

The response is labeled `[YouTube]`, includes the channel, title, and a
`https://youtu.be/...` link. When `BOT_YOUTUBE_API_KEY` is configured, GoBot
also adds the video's public view and like totals when YouTube exposes them.
The statistics are best-effort: a missing like count, an API limitation, or a
temporary statistics lookup failure does not prevent the search result from
being returned. The command searches video results, which includes music
videos and other YouTube video content. GoBot uses the key for the official
Data API search and statistics lookup, then falls back to YouTube's public
results page when the key is unavailable or the API cannot be used. Configure
`plugins.youtube.max_length` and `plugins.youtube.timeout_seconds` as needed.
The API key is optional, but improves search reliability and avoids depending
on changes to YouTube's public results HTML.

## CVE lookup

The `cve` plugin queries the NVD's public CVE 2.0 API and requires no API key:

~~~text
!cve CVE-2024-3094
!vuln CVE-2024-3094
~~~

GoBot returns the CVE ID, the best available CVSS score and severity, up to
three affected vendor/product/version labels, and the NVD detail link. NVD
records may not have a score yet, and CPE applicability data can be broad, so
the response omits fields that are unavailable. The lookup accepts only a
single CVE identifier and never fetches a user-supplied URL. For keyword or
broader searches, use the [NVD CVE search page](https://nvd.nist.gov/vuln/search).

~~~yaml
plugins:
  cve:
    enabled: true
    timeout_seconds: 8
    max_length: 360
~~~

## IP and ASN context

Use `!ip` or `!asn` with an IP address or hostname:

~~~text
!ip 8.8.8.8
!asn 1.1.1.1
!ip resolver.example.net
~~~

The response includes the resolved address, ASN, organization, country,
datacenter/hosting and proxy/VPN/Tor indicators, plus reverse DNS when the
provider supplies it. GoBot uses IP-API's free keyless JSON endpoint and
spaces requests to respect its public rate limit. The free endpoint is HTTP,
so do not use this command for sensitive private data; the lookup target is
sent to IP-API. Hostnames are passed to IP-API for resolution, but arbitrary
URLs, paths, and line breaks are rejected. `!asn` is an output alias for an
address lookup; it does not accept a bare ASN number as a separate BGP query.

~~~yaml
plugins:
  ipinfo:
    enabled: true
    timeout_seconds: 8
    max_length: 320
~~~

## Acronyms

`acronym` is a local, offline text-file expander for common business, medical,
education, government, science, internet/chat, finance, and technology terms:

~~~text
!acronym MTTR
!acro rDNS
!acronym API
!acronym API technology
!acronym htpp
~~~

Entries use `ACRONYM|expansion[|context]` per line. Blank lines and `#` comments
are ignored, matching is case-insensitive, and malformed entries are skipped.
The first entry for an acronym is the common/default meaning; additional
meanings can be selected with a context, such as `!acronym API medical`.
Multi-word keys such as `PCI DSS` are supported. If an exact lookup fails,
GoBot performs a bounded local fuzzy check and returns at most one suggestion,
for example: `[acronym] no exact match for htpp; did you mean HTTP?` It does
not silently substitute the suggestion or make a network request.

The bundled catalog is intentionally local, versioned, fast, and deterministic.
Operators can edit or replace `data/acronyms.txt` to add organization-specific
jargon without an API key or runtime dependency. Its broad common-term catalog
was researched offline using public references including the [Section508
acronym list](https://www.section508.gov/tools/acronyms-abbreviations/), NIST
CSRC, IETF RFCs, and the CNCF Cloud Native Glossary.

~~~yaml
plugins:
  acronym:
    enabled: true
    data_file: "data/acronyms.txt"
    max_length: 320
~~~

## Word scramble

Start a local channel round with:

~~~text
!scramble
!scramble status
~~~

GoBot chooses a local word, scrambles it, and awards the first exact answer in
that channel one persisted karma point. A round expires after five minutes;
`!scramble status` shows the active scramble without revealing the answer.
The word list is local and editable, and the winning karma plus a separate
scramble win record are stored in BoltDB. The bundled list focuses on computing,
networking, security, and operations vocabulary. No external API or answer text
is sent anywhere.

~~~yaml
plugins:
  scramble:
    enabled: true
    data_file: "data/scramble.txt"
    timeout_minutes: 5
    max_length: 240
~~~

## Puzzle games

Start a timed puzzle round in the current channel:

~~~text
!puzzle numbers
!puzzle trivia
!puzzle word
!puzzle logic
!puzzle anagram
!puzzle crossword
!puzzle random
!puzzles
!puzzle status
!puzzle stop
~~~

`numbers` generates a target and six numbers. Players submit arithmetic
expressions as ordinary channel messages, using only the supplied numbers at
most once and the operators `+`, `-`, `*`, `/`, and parentheses. Division may
produce a fraction; answers are compared exactly, so the closest result wins.
An exact answer ends the round immediately.

The other categories use local, operator-maintained catalogs:

- `trivia`: general knowledge questions
- `word`: synonym and antonym prompts
- `logic`: riddles and sequence questions
- `anagram`: unscramble a word from the configured scramble catalog
- `crossword`: short crossword-style clues
- `random`: rotates through all available categories before repeating one

Text answers are normalized for case, punctuation, and spacing. The first
correct answer wins. Invalid or unrelated channel messages are ignored, and
every response is one bounded IRC line. Catalog entries use
`prompt|answer[|alternate;answers]`, with one entry per line. The default files
are `data/puzzles/trivia.txt`, `word.txt`, `logic.txt`, and `crossword.txt`.
Within a channel, clues and anagrams are not repeated until their available
catalog has been used, then that pool starts a new cycle.

The default round lasts 45 seconds and is tracked separately per network and
channel. Active rounds are held in memory and disappear if GoBot restarts; no
network calls or external game data are required.

Optional settings:

~~~yaml
plugins:
  puzzle:
    enabled: true
    timeout_seconds: 45
    max_length: 360
    data_dir: "data/puzzles"
    anagram_file: "data/scramble.txt"
~~~

## Weather

Weather uses Open-Meteo and does not require an API key:

~~~text
!weather Seattle
!wx Seattle
!forecast Seattle
!temp Seattle
!weather set Las Vegas
!weather
!wx
!weather clear
~~~

Use `!weather set <city>` to save a personal default location. Quoted city names
are supported, so `!weather set 'Las Vegas'` works too. After saving it, use
`!weather` or any weather alias without a city. Setting it again replaces the
previous default; `!weather clear` removes it. Defaults are saved by your
authenticated IRC account when available, or by network and nickname, and
survive bot restarts. Set `default_units` to metric or imperial.

## Steam

Steam lookups use Steam's public store and charts endpoints and do not require
an API key:

~~~text
!steam Portal 2
!game Baldur's Gate 3
!steam info 620
!steam info https://store.steampowered.com/app/620/
!steam genre FPS
!steam genre RPG
!steam top
~~~

A title search returns one best match, its Steam store link, and a `more
matches` search link for sequels, DLC, or similarly named games. `genre` opens
the matching Steam tag page. `top` reports the current #1 most-played Steam
game and its recent peak when available, plus a link to the full most-played
charts. Game details are kept concise; if a
requested details response exceeds the configured IRC message limit, GoBot
sends the full response by private message and tells the channel that it is
messaging the requester. Configure `plugins.steam.timeout_seconds` and
`plugins.steam.max_length` as needed.

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
!r top r/linuxmemes
!r r/linuxmemes rising
~~~

The response contains the post title, author, subreddit, score, comment count,
and canonical Reddit URL in one compact message. Both `r/subreddit` and full
Reddit subreddit URLs are accepted. GoBot prefers Reddit's public JSON
endpoint, then falls back to the corresponding public RSS feed when JSON is
temporarily blocked or throttled. RSS does not reliably include scores or
comment counts, so those fields are omitted rather than shown as false zeroes.
For individual posts, a final oEmbed title fallback can still provide the
title when Reddit rate-limits both metadata endpoints.
For subreddit lookups, an optional sort selects the first post from Reddit's
`best`, `hot`, `new`, `top`, or `rising` listing. Put the sort before or after
the subreddit; for example, `!r top r/linuxmemes` returns the current #1 post
from the subreddit’s top listing, while plain `!r r/linuxmemes` remains the
newest-post lookup. Sort modifiers are only for subreddit lookups, not
individual post URLs. Explicit non-default sorts are labeled in the response.
Only recognized Reddit hosts and paths are accepted; arbitrary URL fetching is
not performed by this command.

## GitHub lookup

The GitHub plugin uses the public GitHub REST API and keeps every response to
one bounded IRC message. It accepts repository names, GitHub URLs, issues,
pull requests, releases, commits, users, and repository searches:

~~~text
!github variablenix/GoBot
!gh https://github.com/variablenix/GoBot/issues/1
!github variablenix/GoBot#12
!github variablenix/GoBot releases
!github user octocat
!github search go irc bot
~~~

Repository responses include a short description, stars, forks, open issues,
language, and a link. Issue and pull-request responses include the title,
state, author, comments, and link. Search returns up to three compact
repository matches. Invalid hosts and malformed references are rejected
locally; GitHub response text is cleaned before it reaches IRC.

Authentication is optional. A token can increase the public API rate limit,
but GoBot only performs read-only lookups and never needs repository write
permissions. Keep the token out of `config.yaml` in deployments:

~~~env
BOT_GITHUB_TOKEN=your-optional-read-only-token
~~~

The token is sent only to `api.github.com` over HTTPS. Leave it empty when the
anonymous public API limit is sufficient.

## Grabbed channel lines

The `grab` plugin lets users save memorable lines without keeping a complete
transcript of the channel in memory. GoBot remembers only the latest message
from each nickname in the current channel; a line is written to BoltDB only
when someone explicitly grabs it:

~~~text
!grab Alice
!lgrab Alice
!grabr
!grabr Alice
!grabs deployment
~~~

`!grab <nick>` saves that nickname's latest message. `!lgrab` repeats their
most recently saved line, `!grabr` shows a random saved line, and `!grabs`
searches saved nicknames and text. Search results are limited to three compact
matches so the plugin cannot flood a channel. Duplicate lines are ignored and
each nickname is limited to the configured number of saved lines. The plugin
supports ordinary messages and `/me` actions, strips IRC control characters,
and adds a zero-width separator after the first nickname character to avoid
unwanted highlights.

~~~yaml
plugins:
  grab:
    enabled: true
    max_length: 320
    max_quotes_per_user: 20
~~~

The saved lines are scoped by network and channel and survive restarts. The
plugin does not expose a command to dump the entire database.

## Linux kernel versions

`!linux` and `!kernel` fetch the current release-line summary from
`kernel.org` and return it as one bounded IRC message:

~~~text
!linux
!kernel
~~~

The lookup is read-only, has a short timeout, limits the response body, and
does not accept arbitrary URLs. It requires no API key:

~~~yaml
plugins:
  linux:
    enabled: true
    timeout_seconds: 8
    max_length: 260
~~~

## Foods and drinks

Foods is a local, data-driven suggestion plugin. It does not call a food API
or send user input to a third-party service. Each request produces one short
IRC message:

~~~text
!food
!food ramen
!food pork
!food steak
!beer guinness
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

`!food` accepts a search term as well as a category. For example, `!food pork`
searches the complete catalog for matching dishes, while `!beer guinness`
searches only the beer list. Matching is case-insensitive and supports
multi-word terms. If no item matches, the command keeps its friendly random
suggestion behavior and treats the text as an optional nickname.

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
production span. The bundled catalog includes hundreds of entries from brands
such as Bugatti, Ferrari, Lamborghini, McLaren, Pagani, Porsche, Rolls-Royce,
Koenigsegg, Rimac, Lucid, and major international manufacturers:

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

Pool's separate 8-ball mode uses `!pool 8 <nick>`, `!pool8 <nick>`, or
`!8pool <nick>`; `!8ball` is reserved for questions to the magic 8-ball.

## Doobie countdown

The `doobie` plugin provides a timed, local-only social countdown. Use either
command directly:

~~~text
!doobie
!420
~~~

Standalone `$doobie` or `$420` tokens in a channel message trigger the same
sequence, including punctuation such as `Who wants to smoke a $doobie?`. The
common `$dooblie` spelling is accepted as well. The default output is:

~~~text
📜 3... Grinding...
👅 2... Rolling...
🔥 1... Sparking...
Spark it up and pass it around! 🔥🍁
~~~

The first line is immediate and the remaining lines are separated by the
configured delay. The closing line is selected randomly from the local
`quotes/doobie.txt` catalog and is sent in green. Each sender has a 15-second
cooldown, and only one countdown can run in a channel at a time. No network
calls or database storage are used; pending timers are cancelled if the plugin
is disabled. Configure it under `plugins.doobie` with
`countdown_delay_seconds` (0–60, default 1), `cooldown_seconds` (1–3600,
default 15), and `quotes_file`.

## Fun text catalogs

The `fun` plugin provides local text without making an external request. Each
command returns one randomly selected, bounded line. The yo-momma catalog is
organized around classic joke categories such as size, age, smell, silliness,
work, food, technology, and everyday mishaps. It is playful and non-targeted:
operators should remove any entries that do not fit their channel's tone.

~~~text
!yomomma
!yo
!oneliner
!one
!pun
!puns
!wisdom
!wise
~~~

The catalogs are stored under `data/fun/`:

- `yo_momma.txt`: categorized yo-momma jokes, including fat, skinny, tall,
  short, old, smelly, stupid/silly, loud, broke, clumsy, food, technology,
  school, sports, and other classic joke themes
- `one_liners.txt`: short original one-liners
- `puns.txt`: short original puns
- `wisdom.txt`: original/adapted wisdom lines without questionable attribution

They are intentionally curated rather than presented as an exhaustive list of
every joke or quotation. Add one line per entry; GoBot loads the files when it
starts. The plugin is configured as follows:

~~~yaml
plugins:
  fun:
    enabled: true
    data_dir: "data/fun"
    max_length: 240
~~~

The catalogs are local and editable, so operators can remove a line or adapt
the tone of a channel without changing Go code.

## Attack actions

The `attack` plugin provides short, playful target-based actions and messages:

~~~text
!attack slap Alice
!slap Alice
!hug Alice
!flirt Alice
!compliment Alice
!high5 Alice
!gift Alice
!spank Alice
!attack pokemon Alice
!strax
~~~

The canonical form is `!attack <style> [nick]`; most styles require a nickname.
Aliases include `!bite`,
`!fight`, `!glomp`, `!insult`, `!kill`, `!lart`, `!present`, `!spank`,
`!stab`, `!bdsm`, `!clinton`, `!trump`, `!lurve`, and `!pokemon`, plus
CloudBot-style aliases such as `!end`, `!sexup`, `!jackmeoff`, `!dominate`,
`!spar`, `!challenge`, and `!luff`. `!strax`, `!nk`, and `!westworld`
also work without a target. Action styles use standard IRC CTCP ACTION
formatting, so clients usually render them like `/me` messages; compliment,
flirt, insult, lurve, pokemon, strax, nk, and westworld use ordinary messages.
Targets must be a single IRC-style nickname. Targeting GoBot or `self` makes
GoBot perform the playful action toward the sender. Templates are built in,
capped to short safe text, and contain no real-world instructions.

## Firearm and weapons catalog

The `weapons` plugin is a lightweight reference/randomizer for firearm and
weapons terminology. It returns names and broad categories only; it does not
provide instructions, construction details, acquisition advice, or tactical
guidance. Generic commands choose from the whole catalog, while category
aliases narrow the result:

~~~text
!firearm
!guns pistol
!rifle
!shotgun
!smg
!grenade
!explosives
~~~

The catalog includes civilian, historical, sporting, military, launcher,
grenade, and high-level explosive terms. Keep the default output limit in
place to ensure one short IRC response. Operators can edit `data/weapons.txt`
or disable the plugin with `plugins.weapons.enabled: false`.

~~~yaml
plugins:
  weapons:
    enabled: true
    data_file: "data/weapons.txt"
    max_length: 240
~~~

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

## Ask / questions

Ask GoBot a general question with any of these aliases:

~~~text
!ask how dangerous is extreme dehydration?
!question what is the Linux kernel?
!q what does TLS protect?
~~~

The `!ask` plugin uses DuckDuckGo Search Assist first when
`search_assist_enabled` is enabled. This is a best-effort integration of the
Search Assist request used by DuckDuckGo's web results, not a documented public
API; it may be disabled if DuckDuckGo changes the page or endpoint. The plugin
then uses a local Chromium/Chrome browser to read the rendered Search Assist
card when the lightweight request has no answer, followed by DuckDuckGo's
keyless Instant Answer endpoint and Wikidata as fallbacks. Search Assist and DuckDuckGo summaries may be backed by encyclopedia
sources; GoBot presents them as bounded answers and links to a cited source
when one is provided. Query framing is normalized across common definition,
relationship, origin, release, publication, debut, launch, genre, and date phrases.
Relationship and temporal questions use intent checks and structured Wikidata
claims where available, so a generic entity description is not reused as the
answer to a specific person or date question. Search Assist also tries one
bounded, intent-preserving framing variant for indirect opinion questions.
Relationship claims cover
creators, developers, founders, authors, inventors, and directors. For
open-ended questions that do not produce a structured answer, the plugin can
inspect the rendered DuckDuckGo result cards. It selects the most relevant
visible result, fetches a bounded public HTML excerpt (or Reddit's public JSON
for a Reddit post), and attributes the excerpt to that result. This is source
text rather than an AI-generated synthesis; if fetching is unavailable, the
DuckDuckGo snippet is used. A sender-specific cooldown prevents repeated
lookups from becoming a flood source.

The plugin is configured under `plugins.ask`:

~~~yaml
plugins:
  ask:
    enabled: true
    search_assist_enabled: true
    search_assist_browser_enabled: true
    search_results_enabled: true
    browser_path: ""
    duckduckgo_enabled: true
    wikidata_fallback: true
    max_length: 360
    max_response_chars: 240
    timeout_seconds: 8
    cooldown_seconds: 15
~~~

No credential or `.env` setting is required. The rendered fallback requires a
Chromium/Chrome executable; Docker images include Chromium, while systemd
deployments should install Chromium or set `browser_path`. Search Assist is a
best-effort integration of DuckDuckGo's web request rather than a documented
API, so it can be disabled if DuckDuckGo changes the endpoint. When
`search_results_enabled` is true, the same browser fallback may read normal
result cards and fetch only public HTTP(S) pages on ports 80/443, with bounded
timeouts and response sizes. Private, local, and non-HTTP destinations are
rejected. The response is sanitized,
limited to one IRC line, and includes a cited source link when available. If no
provider has a usable answer, GoBot returns a bounded DuckDuckGo search link
instead of inventing one.

## Daily bonus

The `daily` plugin adds a persistent `!daily` command:

~~~yaml
plugins:
  daily: {enabled: true, bonus_xp: 25}
~~~

Each authenticated account can claim once per UTC calendar day, regardless of
channel or network. Users without an authenticated account are limited by
network and nickname. Different users may claim independently; this is not a
single shared server-wide claim. The reward is added to the Duck Hunt XP
profile for the channel where the claim is made, so Duck Hunt must be enabled
globally and in that channel. Claims survive restarts through the configured
Bolt database. Daily tracks the consecutive UTC-day claim streak separately
from Duck Hunt's XP and level progression.

After changing `config.yaml`, an owner can send GoBot a private `reload`
message to apply reloadable plugin settings without reconnecting. Changes to
`.env` still require a service restart because systemd reads that file when the
process starts.

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
!luv nickname
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
- luv awards the named nickname one persistent blue-heart point with `!luv
  nickname`; totals are case-insensitive and scoped to the current IRC network.
  Responses use 💕 for the kindness action and 💙 for the recipient's score,
  for example: `💕 me spreads kindness to nick. nick now has 58 💙 points!`
- thing++ and thing-- also work as standalone ordinary chat messages. GoBot
  confirms each update in one compact line, replacing `<nickname>` with the
  actual nickname being changed, such as
  `🆙 Karma boost! KnownSyntax gained 1 karma ✨ (🎯 1 in #chat | 🌐 2 global)`;
  command messages are not treated as karma changes.
- In a channel, Karma reports the channel total and the global total, for
  example `🎯 9 in #chat | 🌐 19 global`; private messages report global Karma.
- Positive karma milestones at +10, +25, +50, and +100 add a highlighted
  trophy notice when the total crosses the threshold.
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
