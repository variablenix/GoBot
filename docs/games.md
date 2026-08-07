# Games and activities

Games use the same command cooldown and outbound queue protections as every
other plugin. No game uses real money or wagering. Duck Hunt points are local
scores with no value outside the bot.

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
combines short randomized duck appearances with a small points-and-gear arcade
loop. Points have no value outside the bot; there are no wagers or external
payments.

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
    bread_cost: 8
    # Points are shop currency; XP is separate and controls player levels.
    xp_per_hit: 5
    xp_per_kill: 25
    xp_per_befriend: 10
    flock_min: 2
    flock_max: 4
~~~

Once the activity threshold is reached, GoBot waits a random amount of time
and announces a duck. Players can then shoot it over multiple hits or build
trust through repeated befriending attempts:

~~~text
[Duck Hunt] · ° · ° · ° \_o< FLAP FLAP!
[Duck Hunt] \_o< QUACK! HP: 3 | Type !bang to shoot or !bef to befriend!
username hit the GOLDEN DUCK for 1 damage! It has 2 HP left. +20 points
A flock of 2 ducks has landed! Type !bang to pick them off!
username hit a duck in the flock for 1 damage! It has 0 HP left. +10 XP
2 ducks still in the flock!
~~~

The active-duck announcement stays compact for terminal clients: `\_o< QUACK!`.
When IRC colors are supported, the duck uses a soft tan approximation, green
head, and yellow bill. The `GOLDEN DUCK` label is reserved for hit, befriending, and
escape messages, where it is shown in yellow; it is not inserted into the duck
ASCII itself.

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
!ducklaunch flock      launch a random flock manually
!shop / !store        list arcade gear, consumables, and point prices
!buy peashooter       buy the starter Peashooter (!buy gun also works)
!buy quacker          buy the larger Quacker Blaster
!buy golden           buy the stronger Golden Wing
!buy 1-7              buy a Duck Hunt consumable by numeric item ID
!buy magazine         buy a spare magazine (`!buy mag` also works)
!use <1-7|item>       use a Duck Hunt consumable
!ammo                 show ammo, spare magazines, and points
!reload               load a spare magazine
!ducks [nickname]     show persistent scores, points, level, and XP
!duckstats [nickname] show detailed title, accuracy, gear, and item stats
!level [nickname]     show level, XP, points, and Duck Hunt scores
!xp [nickname]        alias for !level
!profile [nickname]   alias for !level
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
ducks are less common and award a larger points bonus.

`!ducklaunch flock` manually starts a flock with a random size between
`flock_min` and `flock_max` (2–4 by default). Each duck gets its own random HP
within the configured HP range. A final hit on one flock member reports the
reward and how many ducks remain; the flock stays active until every member is
resolved. Automatic activity spawns remain single ducks. Launching while a
hunt is active is rejected so an existing round cannot be overwritten. Use
`!ducklaunch` without `flock` for the usage hint.

`!bef` uses a small trust progression. The duck can warm up to a user without
being tamed immediately; after `befriend_attempts` successful approaches, the
user befriends it and receives a points bonus. Successful shots also earn
points, and resolving a duck adds a larger completion bonus. Spend those
points in `!shop`: the Peashooter is the baseline item, the Quacker Blaster
has a larger magazine, and the Golden Wing costs more but deals extra damage.
The catalog uses arcade-style names and simple game statistics so the feature
stays focused on gameplay rather than real-world equipment.

`!duckstats` adds a detailed profile without changing the existing `!ducks`,
`!level`, `!xp`, or `!profile` commands. Titles progress from Pond Rookie at
level 1 through Duck Whisperer at level 6, Legendary Hunter at level 10, and
Eternal Duckkeeper at level 25. Levels continue beyond that title, but the top
title requires 30,000 XP under the default cumulative progression curve.
Stats include shots fired, ducks killed and befriended, shot accuracy (hits
divided by shots), hit rate (kills divided by successful hits), armed state,
ammo, spare magazines, and inventory. Jam chance is currently 0% because
Duck Hunt does not yet implement weapon jams. It also reports ready one-shot
effects such as a polished shot or Golden Seed bounty.

Duck Hunt has seven local consumables. Buy them with `!buy <item>` or their
numeric IDs, then activate them with `!use <item>`:

- `1` Lucky Feather (12 points): raises the next automatic duck's golden chance
  for 20 minutes; additional feathers stack up to a +75 percentage-point boost.
- `2` Duck Whistle (18 points): schedules the next automatic duck in 15 seconds.
- `3` Decoy Duck (10 points): extends an active hunt by 20 seconds.
- `4` Gun Brush (12 points): guarantees your next armed shot, if you own a gun.
- `5` Bread (8 points by default): makes automatic spawns 2.0x faster for 20
  minutes. Additional uses stack to 2.5x and then 3.0x.
- `6` Golden Seed (20 points): adds 25 points and 25 XP to your next completed
  kill.
- `7` Pond Map (15 points): turns the next automatic duck visit into a random
  flock.

Item aliases include `feather`, `whistle`, `decoy`, `brush`, `bread`, `seed`,
and `map`. Effects that cannot currently be applied are not consumed. Timed
channel effects expire with their timers or when the bot restarts; Bread affects
automatic activity scheduling, not manually launched flocks.

Points and XP are separate persistent values. Points are the shop currency
used by `!buy` and `!reload`; XP measures Duck Hunt progress and determines the
player's level. A successful kill is worth more than befriending by default:
`xp_per_kill` is 25 XP and `xp_per_befriend` is 10 XP, while the corresponding
point bonuses are 10 and 5. Duck Hunt does not award player HP. Everyone starts
at level 1. Level 2 requires 100 total XP,
level 3 requires 300 total XP, level 4 requires 600 total XP, and each later
level follows the same gradually increasing curve. Normal hits, kills, and
befriending award the configured XP values; golden ducks award double XP.
Use `!level` or `!profile` to see your own profile, or add a nickname to view
another player's public Duck Hunt totals. Existing BoltDB player records are
compatible: players from older versions simply begin with 0 XP and keep their
existing points, gear, and scores.

New players receive `starting_points`. A player can own one weapon at a time;
buying a different weapon replaces the current gear and starts a fresh magazine.
Consumables such as Bread remain in the inventory. Spare magazines are purchased with `!buy magazine` or `!buy mag` and loaded with
`!reload`.
Ammo is consumed by `!bang`. When a player runs empty, GoBot says to use
`!reload` if a spare magazine is available or `!buy magazine`/`!buy mag` to purchase one;
successful hits also include that reminder when the shot empties the magazine.
Successful hits and befriending replenish the player's points over time. If a
player fires when no duck is active, the
bot may confiscate their gear and report `[GUN CONFISCATED]`; the player can
earn more points and visit `!shop` again. This is only a game-state penalty.

If nobody shoots or befriends the duck before `timeout_seconds` expires, it
responds with one randomized escape line such as `The duck escapes into the
sky!` or `\\_o< *ZOOM* The speedy duck vanishes in a flash!`. This is one IRC
message, not a follow-up flood. Escape results include the compact colored
duck ASCII and a highlighted motion line, such as `\\_o< The duck flaps away,
living another day. °°°...`. The output deliberately uses ASCII motion instead
of emoji so it stays readable in terminal IRC clients; clients without color
support still receive readable text and an ASCII duck.

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
- firearm_enabled: enable the arcade gear and ammo sub-game
- magazine_size and starting_ammo: baseline Peashooter capacity and ammunition
  loaded when a player buys gear; the Quacker Blaster and Golden Wing adjust
  the baseline capacity/damage
- starting_points: points granted when a player first participates
- magazine_cost and gun_cost: base shop prices; the enhanced items add their
  own small point premium
- bread_cost: points required to buy one Bread consumable; use `!use 5` or
  `!use bread` to accelerate future automatic spawns
- xp_per_hit: XP awarded for a successful non-final hit; golden ducks double it
- xp_per_kill: XP awarded when a shot resolves the duck; golden ducks double it
- xp_per_befriend: XP awarded when a player completes the trust progression;
  golden ducks double it
- flock_min and flock_max: inclusive random flock size for `!ducklaunch flock`;
  automatic activity spawns remain single ducks

Scores, points, XP, levels, and player gear are stored in BoltDB and survive
restarts. The progression data is local to each network/channel/nickname.
After a duck is resolved or times out, the channel must reach the activity
threshold again before another automatic event is scheduled.
