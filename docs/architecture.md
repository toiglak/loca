# Architecture

Loca is a macOS-local HTTPS router. It maps stable `.localhost` names to services listening on loopback ports and keeps the routing layer separate from the applications it serves.

## Runtime model

The normal installation runs Loca as a persistent `launchd` service. macOS owns the privileged low ports, while the router process runs as the logged-in user rather than as root. Socket activation lets the service restart after a crash or system boot without requiring a terminal to remain open.

Listeners are restricted to `127.0.0.1` and `::1`. Loca does not expose services to the LAN or the public internet.

The application bundle is installed at `~/Applications/Loca.app`. The bundle identity is used for service registration so the daemon is attributed to Loca rather than to the Go toolchain or an interpreter.

## Address and routing model

Every valid name ends in `.localhost`. Nested names such as `app.project.localhost` are supported. The `.localhost` namespace is reserved for loopback use, so Loca does not need DNS configuration, hosts-file edits, or production-domain aliases.

Persistent mappings associate a name with a local TCP port. Temporary development-service registrations use a separate store and take precedence over a persistent mapping with the same name while the registration is active. Each temporary registration has an owner, so a server can remove only its own route when it closes.

Requests are proxied exclusively to `127.0.0.1:<port>`. HTTP redirects to HTTPS, original `Host` and `Origin` values are preserved, forwarded headers are rebuilt, and WebSocket upgrades and streaming responses pass through the proxy. Unknown aliases and unavailable browser page routes receive a waiting page that can reload when the route becomes available; other requests retain normal error responses.

## HTTPS

Loca creates one ECDSA certificate authority in its local data directory and issues short-lived certificates for valid `.localhost` names on demand. Certificates can be issued before an alias has a registered upstream, which allows a browser to reach the stable address while a service is still starting.

`loca trust` adds the public CA certificate to the logged-in user's macOS trust settings. The private CA key remains local with mode `0600`, data directories use mode `0700`, and no external certificate authority is contacted.

Trust is separate from daemon installation because macOS requires the user's interactive security context for keychain changes. Browsers and system tools that use the macOS trust store work without additional configuration. Applications with separate trust stores need the public CA configured explicitly; disabling TLS verification is not a substitute for trust setup.

## Control API

The loopback-only control API is available at `http://localhost/_loca/v1` through the existing HTTP listener.

- `GET /health` reports the API version and service identity.
- Authenticated `POST /dev-services` creates a temporary registration.
- Authenticated `DELETE /dev-services` removes a temporary registration.

Mutations require the per-user bearer token stored in `control.token`. Browser-originated requests are rejected. The control API does not add another listener or change the router's ports.

## Data and lifecycle

Loca stores its data in `~/Library/Application Support/loca/` by default. `LOCA_DATA_DIR` can override the location before installation. The directory contains persistent mappings, temporary registrations, the local CA, and daemon logs.

Mapping writes use a process lock, `fsync`, and atomic rename. Requests read current mappings, so changes become active without restarting the router.

Uninstalling removes the `launchd` registration but intentionally retains mappings, the CA, and the application. To remove everything, uninstall the service, delete the application and CLI symlink, remove the Loca CA from Keychain Access, and delete the data directory. Never share the private CA key.

## Future work

- TODO: compare Loca with similar tools and document the tradeoffs. Candidates include Localias, Caddy, Traefik, and Cloudflare Tunnel.
