# Security notes

GoBot is designed for a self-hosted deployment, but no application can be
declared completely vulnerability-free. Keep the host, Go toolchain,
dependencies, and deployment configuration maintained.

- Keep IRC certificate verification enabled with verify_cert: true.
- GoBot refuses to send SASL or NickServ credentials over non-TLS IRC.
- Keep secrets in .env or a deployment secret store, never in Git.
- Use authenticated IRC account names for owner controls; nicknames are not
  authorization proof.
- Restrict /stats and /metrics; they have no built-in authentication.
- Bind the stats listener to localhost unless another host must scrape it.
- If remote scraping is required, use a private/WireGuard address and firewall
  the port to the Prometheus host.
- The URL title plugin rejects loopback, private, link-local, multicast, and
  local host targets to reduce SSRF risk.
- External HTTP lookups use timeouts and bound response sizes.
- IRC invitations, command handling, and cooldown warnings are rate-limited.
- The Docker image runs as a non-root user.
- Protect the BoltDB data file and its containing directory with filesystem
  permissions.
- Review CI, CodeQL, Dependabot, and vulnerability-scan results before
  deploying updates.
