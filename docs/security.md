# Security notes

GoBot is designed for a self-hosted deployment, but no application can be
declared completely vulnerability-free. Keep the host, Go toolchain,
dependencies, and deployment configuration maintained.

- Keep IRC certificate verification enabled with verify_cert: true.
- GoBot refuses to send SASL or NickServ credentials over non-TLS IRC.
- Keep client certificate/private-key PEM files outside Git with mode 0600 and
  readable only by the GoBot service user.
- CertFP fingerprints are registered with NickServ; do not store a fingerprint
  or private key in the repository.
- The temporary `certfp_enroll` mode uses the existing SASL password only to
  send a one-time NickServ `CERT ADD`; disable it after NickServ confirms the
  certificate so the normal passwordless EXTERNAL flow is used.
- Keep secrets in .env or a deployment secret store, never in Git.
- Use authenticated IRC account names for owner controls; nicknames are not
  authorization proof.
- GoBot requests the IRCv3 `account-tag` capability when the server advertises
  it, and uses that tag for owner checks; if the server does not provide it,
  GoBot does not fall back to nickname-based ownership.
- GoBot also requests IRCv3 `server-time` when available so persisted message
  timestamps reflect the IRC server clock.
- The private `reload` command is accepted only from an authenticated account
  listed in `owner_accounts`; it cannot change ownership or connection
  settings.
- Configuration reloads do not reread systemd's `EnvironmentFile`; restart the
  service after changing `.env` so API keys and provider settings take effect.
- Restrict /stats and /metrics; they have no built-in authentication.
- Bind the stats listener to localhost unless another host must scrape it.
- If remote scraping is required, use a private/WireGuard address and firewall
  the port to the Prometheus host.
- The URL title plugin rejects loopback, private, link-local, multicast, and
  local host targets to reduce SSRF risk.
- External HTTP lookups use timeouts and bound response sizes. Package, audit,
  and Docker requests use fixed public provider hosts; package suggestions use
  only the fixed public Go, npm, and PyPI index hosts.
- The paste plugin's URL mode is different: it fetches a user-supplied HTTP or
  HTTPS URL from the bot host and follows redirects. Treat it as an outbound
  network capability. Only enable it where users are trusted and host/network
  egress rules prevent access to loopback, private, link-local, metadata, and
  other sensitive services; otherwise disable `plugins.paste.enabled`.
- The paste token is sent only to the configured Opengist base URL and should
  use HTTPS. `BOT_PASTE_TOKEN` and `BOT_PASTE_BASE_URL` belong in the service
  environment, not in Git.
- IRC invitations, command handling, and cooldown warnings are rate-limited.
- The Docker image runs as a non-root user.
- Protect the BoltDB data file and its containing directory with filesystem
  permissions.
- Review CI, CodeQL, Dependabot, and vulnerability-scan results before
  deploying updates.
