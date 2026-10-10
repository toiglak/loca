# Loca

Loca gives local services stable HTTPS addresses such as `https://app.localhost` on macOS.

It is a small local router for development. Services keep their normal loopback ports while Loca provides trusted, memorable names that work with browser features such as secure cookies and HTTPS-only APIs.

## Features

- Stable `.localhost` addresses for local services
- Local HTTPS with a user-trusted development certificate authority
- Persistent port mappings that update without restarting the router
- Temporary registrations for development servers such as Vite
- WebSocket and streaming proxy support
- Loopback-only listeners; nothing is exposed to the LAN or public internet

## Quick start

Loca currently supports macOS. Building the application requires Go and Apple Command Line Tools.

```sh
./scripts/install.sh
loca trust
loca set app 5173
loca install
open https://app.localhost
```

Add `~/.local/bin` to your `PATH` if the `loca` command is not found after installation.

## Everyday commands

```sh
loca set app 5173       # create or update a mapping
loca remove app         # remove a mapping
loca list               # list mappings and upstream reachability
loca open app           # open https://app.localhost
loca status             # show the macOS service state
loca doctor             # diagnose installation and trust issues
loca stop               # stop the service
loca uninstall          # remove the service registration
```

For a foreground diagnostic run, use `loca run`.

## Vite integration

Loca includes the [`vite-plugin-loca`](packages/vite-plugin-loca/README.md) package. Add it to a Vite configuration to register a development or preview server under a stable `.localhost` address:

```ts
import { defineConfig } from "vite";
import loca from "vite-plugin-loca";

export default defineConfig({
	plugins: [loca("app.localhost")],
});
```

The plugin falls back to Vite's normal local URL when Loca is unavailable. Consumers that require the stable address can pass `{ required: true }`.

## Documentation

- [Architecture and security](docs/architecture.md)
- [Development and testing](docs/development.md)
- [Vite plugin usage](packages/vite-plugin-loca/README.md)

## License

Loca is released under the [MIT License](LICENSE).
