# NickServ and SASL authentication

NickServ registration and SASL authentication are separate steps:

1. register the nickname/account on the IRC network
2. configure GoBot to log into that account automatically

Connect with a normal IRC client and check the network's NickServ help:

~~~text
/msg NickServ HELP REGISTER
~~~

Many networks use a flow similar to:

~~~text
/msg NickServ REGISTER <strong-password> <email-address>
/msg NickServ IDENTIFY <account-name> <strong-password>
~~~

After registering, configure the bot with the registered nickname and SASL
account name. Keep the password in .env when using SASL PLAIN:

~~~yaml
identity:
  nick: GoBot
  sasl_mechanism: plain
  sasl_user: GoBot
  sasl_pass: ""
~~~

~~~env
BOT_SASL_USER=GoBot
BOT_SASL_PASS=replace-with-your-nickserv-password
~~~

Other optional deployment variables:

~~~env
BOT_NEWS_API_KEY=
BOT_LASTFM_API_KEY=
BOT_YOUTUBE_API_KEY=
BOT_STORAGE_DB_PATH=./data/bot.db
BOT_STATS_LISTEN_ADDRESS=127.0.0.1:8082
~~~

Protect the file:

~~~sh
chmod 600 .env
~~~

Notes:

- BOT_SASL_USER and BOT_SASL_PASS override config values.
- BOT_SASL_PASS authenticates the bot; it does not register a nickname.
- Never commit NickServ passwords.
- Keep verify_cert: true.
- GoBot refuses to send credentials over a non-TLS IRC connection.

## OuchNet CertFP / SASL EXTERNAL

OuchNet supports passwordless account authentication with a TLS client
certificate. Its [CertFP instructions](https://ouch.chat/nickserv/certfp.html)
show how to create an Ed25519 certificate and add its fingerprint to NickServ.
The fingerprint itself is stored by NickServ; GoBot reads the certificate and
private key and presents them during the TLS handshake.

For OuchNet's combined PEM format, configure the certificate path under the
network's server settings:

~~~yaml
server:
  host: closet.ouch.chat
  port: 6697
  tls: true
  verify_cert: true
  client_cert: /home/ak/irc-bot/GoBot/secrets/echo-ouch.pem
identity:
  nick: Echo
  sasl_mechanism: external
  sasl_user: ""
  sasl_pass: ""
~~~

`client_key` is optional when the certificate and private key are combined in
one PEM file. If they are separate, set both `client_cert` and `client_key`.
Keep the private key outside Git, readable only by the GoBot service user, and
do not configure the certificate fingerprint in GoBot. The IRC server derives
the fingerprint from the presented certificate.

For one-network deployments, these environment variables are also supported:

~~~env
BOT_SASL_MECHANISM=external
BOT_TLS_CLIENT_CERT=/home/ak/irc-bot/GoBot/secrets/echo-ouch.pem
BOT_TLS_CLIENT_KEY=
~~~

The server must advertise SASL and the `EXTERNAL` mechanism. All DNS backends
behind a shared IRC hostname should expose the same capability set.

If a network renames the bot to Guest..., review the optional nickserv_fallback
and nickserv_ghost identity settings.
