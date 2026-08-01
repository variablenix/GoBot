# Games and activities

Games use the same command cooldown and outbound queue protections as every
other plugin. No game uses real money or virtual currency.

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
does not kick users, use virtual currency, or involve wagers.

~~~yaml
plugins:
  duckhunt:
    enabled: true
    minimum_messages: 25
    minimum_users: 2
    min_delay_seconds: 60
    max_delay_seconds: 300
    timeout_seconds: 30
    flavor_enabled: true
    flavor_min_lead_seconds: 15
    befriend_enabled: true
    min_reaction_seconds: 1
    retry_cooldown_seconds: 7
~~~

Once the activity threshold is reached, GoBot waits a random amount of time
and announces a duck. The first person to shoot or befriend it wins:

~~~text
[Duck Hunt] · ° · ° · ° \_o< FLAP FLAP!
A wild duck appeared: \_o< QUACK! Type !bang to shoot it!
username shot a duck in 2.137 seconds! You have killed 1 duck in #example.
~~~

Before the duck appears, GoBot may send one randomly timed, colorized teaser
such as a flight trail, quack, or flap. There is at most one teaser per hunt
cycle, and `flavor_min_lead_seconds` keeps it separated from the actual duck
announcement so it does not turn into channel chatter.

Commands:

~~~text
!bang                 shoot an active duck
!bef                  befriend an active duck, if enabled
!befriend             same as !bef
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
Each user gets a short retry cooldown. Invalid shots when no duck is active are
quietly ignored.

Announcements and results use standard mIRC IRC colors for the Duck Hunt
label, duck, quack, misses, and successful interactions. Clients without color
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

Scores include ducks shot and ducks befriended, are stored in BoltDB, and
survive restarts. After a duck is resolved or times out, the channel must
reach the activity threshold again before another automatic event is scheduled.
