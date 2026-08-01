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
account name. Keep the password in .env:

~~~yaml
identity:
  nick: GoBot
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

If a network renames the bot to Guest..., review the optional nickserv_fallback
and nickserv_ghost identity settings.
