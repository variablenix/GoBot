# Games and activities

Games use the same command cooldown and outbound queue protections as every
other plugin. No game uses real money or wagering; some activities, such as
Duck Hunt, use fictional points for local scoring only.

## Blackjack / 21

Start a separate per-user game in the current channel:

~~~text
!21
!bj
!blackjack
~~~

During a game:

- !21 hit or !hit draws another card
- !21 stand or !stand lets the dealer finish
- !21 double or !double draws one final card and stands; it is only available
  on the initial two-card hand

View persistent records:

~~~text
!bj stats
!bj leaderboard
~~~

Games are tracked separately per nickname and channel. Active hands are kept
in memory and disappear if the bot restarts; abandoned games expire after 30
minutes. The dealer stands on 17.

Blackjack records are stored in BoltDB and survive restarts. GoBot tracks
hands, wins, losses, pushes, blackjacks, busts, win rate, and streaks.
Authenticated IRC account names are preferred for identity, with nicknames as
fallback.

Replies use standard IRC colors where supported and UTF-8 suit symbols such as
♠, ♥, ♦, and ♣. Clients without color or Unicode support still receive
readable ranks and results.

## 8-ball and 9-ball pool

Pool is a compact, turn-based simulation for two people in a channel. It uses
legal target selection and randomized pocket/miss outcomes rather than trying
to reproduce graphical billiards physics. It has no wagers, virtual currency,
or real-money mechanics.

Challenge another nickname and have them accept:

~~~text
!pool 8 Alex
!pool 9 Alex
!pool accept
!pool decline
~~~

Pool commands are intentionally separate from the magic 8-ball plugin. Use
`!pool 8 <nick>`, `!pool8 <nick>`, or `!8pool <nick>` for 8-ball pool. Use
`!pool 9 <nick>`, `!pool9 <nick>`, or `!9ball <nick>` for 9-ball. Use
`!8ball <question>`, `!8`, or `!eightball` for a magic 8-ball answer.

During a game, the player whose turn it is shoots one numbered ball:

~~~text
!pool status
!pool shoot 3
!pool shoot 8
!pool forfeit
~~~

In 8-ball, balls 1–7 are solids, 9–15 are stripes, and the 8-ball becomes
legal only after the player's assigned group is cleared. In 9-ball, the lowest
remaining ball must be selected first; pocketing the 9-ball wins. A successful
shot keeps the turn, while a miss passes it to the other player.

Aliases include `!pool8`, `!8pool`, `!pool9`, `!9ball`, `!9`, `!pool break`,
and `!shoot <ball>`.
The invited player can use `!pool decline` if they do not want to play.
Use `!poolstats [nick]` for persistent wins and losses or
`!poolleaderboard` for the top five players. Active games are held in memory
and expire after the configured game or turn timeout; completed scores are
stored in BoltDB and survive restarts.

Optional settings:

~~~yaml
plugins:
  pool:
    enabled: true
    game_timeout_minutes: 30
    turn_timeout_seconds: 120
    shot_success_percent: 65
~~~

`65` is a good all-around default for casual play: shots succeed often enough
to keep a game moving, while misses still make the outcome unpredictable. Use
roughly `45`–`55` for a tougher, slower game, `75`–`85` for a more forgiving
game, or `100` when deterministic success is useful for testing. The value is
only a fun simulation probability; it does not measure player skill or alter
the legal target-selection rules. All responses remain single IRC messages.

## Polls

Create a poll with two or more options:

~~~text
!poll create Pizza or tacos? | Pizza | Tacos
!poll vote 1
!poll results
!poll close
~~~

Each nickname gets one vote; voting again changes that vote. Active polls are
stored in BoltDB and restored after restart. Polls automatically expire after
15 days.

## Reminders

~~~text
!remind 45s check the connection
!remind 30m check the logs
!remind 1h30m check the deployment
!remind 72h check in three days
~~~

Durations accept Go-style s, m, and h units from one second through 360 hours.
Use 72h for three days because d is not a valid Go duration. There are at most
20 pending reminders per user/channel. Pending reminders are stored in BoltDB
and rescheduled after restart; reminders older than 15 days are discarded.

## Duck Hunt

Duck Hunt is an optional channel activity event. It is disabled by default and
does not kick users, use real money, or involve wagers. Its points and gear are
fictional arcade mechanics only.

~~~yaml
plugins:
  duckhunt:
    enabled: true
    minimum_messages: 25
    minimum_users: 2
    min_delay_seconds: 60
    max_delay_seconds: 300
    timeout_seconds: 60
    flavor_enabled: true
    flavor_min_lead_seconds: 15
    befriend_enabled: true
    min_reaction_seconds: 1
    retry_cooldown_seconds: 7
    minimum_hp: 1
    maximum_hp: 5
    damage_per_shot: 1
    befriend_attempts: 3
    golden_duck_probability: 0.15
    firearm_enabled: true
    magazine_size: 6
    starting_ammo: 6
    starting_points: 25
    magazine_cost: 15
    gun_cost: 25
~~~

Once the activity threshold is reached, GoBot waits a random amount of time
and announces a duck. Players can then shoot it over multiple hits or build
trust through repeated befriending attempts:

~~~text
[Duck Hunt] · ° · ° · ° \_o< FLAP FLAP!
[Duck Hunt] \_o< GOLDEN DUCK QUACK! HP: 3 | Type !bang to shoot or !bef to befriend!
username hit the GOLDEN DUCK for 1 damage! It has 2 HP left. +20 points
~~~

Before the duck appears, GoBot may send one randomly timed, colorized teaser
such as a flight trail, quack, or flap. There is at most one teaser per hunt
cycle, and `flavor_min_lead_seconds` keeps it separated from the actual duck
announcement so it does not turn into channel chatter.

Duck Hunt includes several randomized messages: four flight-trail/flavor
teasers, multiple duck and quack announcement variants, and escape actions for
flying, flapping, waddling, slipping through reeds, zooming away, and the ninja
duck's smoke-bomb exit. A hunt uses at most one teaser and one final outcome
message, so the variety does not create a flood.

Commands:

~~~text
!bang                 shoot an active duck
!bef                  befriend an active duck, if enabled
!befriend             same as !bef
!buy gun              buy the fictional duck gun
!buy magazine         buy a fictional spare magazine
!ammo                 show ammo, spare magazines, and points
!reload               load a spare magazine
!ducks [nickname]     show persistent scores
!dh                   show Duck Hunt status
!dh status            show Duck Hunt status
!dh start             enable automatic activity for this channel (owner only)
!dh stop              stop Duck Hunt in this channel (owner only)
~~~

!dh is an alias for !duckhunt. Start and stop require an authenticated IRC
account listed in owner_accounts; anyone can shoot or befriend an active duck.

Instant shots are treated as likely scripted input and miss. Shots during the
early reaction window have a probability of success; slower shots succeed.
Each user gets a short retry cooldown. A successful shot removes
`damage_per_shot` HP; the duck remains active until its HP reaches zero. Golden
ducks are less common and award a larger fictional points bonus.

`!bef` uses a small trust progression. The duck can warm up to a user without
being tamed immediately; after `befriend_attempts` successful approaches, the
user befriends it and receives a fictional points bonus. If there is no active
duck, `!bang` gives a short no-duck response. If the user had fictional arcade
gear, it may be confiscated as part of the joke; this has no effect outside the
bot's game data.

If nobody shoots or befriends the duck before `timeout_seconds` expires, it
responds with one randomized escape line such as `The duck escapes into the
sky!` or `\\_o< *ZOOM* The speedy duck vanishes in a flash!`. This is one IRC
message, not a follow-up flood. Announcements and results use standard mIRC
IRC colors for the Duck Hunt label, duck, quack, misses, and successful
interactions. Clients without color support still receive readable text and an
ASCII duck.

Settings:

- minimum_messages: messages required before a spawn can be scheduled
- minimum_users: distinct nicknames required before a spawn can be scheduled
- min_delay_seconds and max_delay_seconds: random wait after the threshold
- timeout_seconds: how long the duck remains available
- flavor_enabled: enable one random pre-spawn teaser per activity cycle
- flavor_min_lead_seconds: minimum separation before the duck announcement;
  it also prevents the teaser from appearing immediately after the threshold
- befriend_enabled: whether !bef and !befriend are available
- min_reaction_seconds: minimum reaction time before a shot can succeed
- retry_cooldown_seconds: per-user cooldown after a shot attempt
- minimum_hp and maximum_hp: inclusive range for a duck's starting HP
- damage_per_shot: HP removed by each successful shot
- befriend_attempts: trust approaches needed to befriend the duck
- golden_duck_probability: chance that a spawn is a higher-value golden duck
- firearm_enabled: enable the fictional duck-gun/ammo sub-game
- magazine_size and starting_ammo: magazine capacity and ammunition loaded when
  a new fictional gun is acquired
- starting_points: fictional points granted when a player first participates
- magazine_cost and gun_cost: fictional point costs for arcade gear

Scores, fictional points, and player gear are stored in BoltDB and survive
restarts. After a duck is resolved or times out, the channel must reach the
activity threshold again before another automatic event is scheduled.
