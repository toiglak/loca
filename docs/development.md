# Development

## Prerequisites

- macOS for the daemon and installation workflow
- Go 1.23 or newer
- Apple Command Line Tools
- Node.js 20.19 or newer for the Vite plugin package

## Build and install locally

```sh
./scripts/install.sh
```

The installer builds and ad-hoc signs the macOS application, stages the service configuration, and creates the `loca` CLI symlink. Restart the service after rebuilding to load a new binary.

## Tests

Run the Go tests with the race detector and static analysis:

```sh
go test -race ./...
go vet ./...
```

Run the Vite plugin tests from its package directory:

```sh
cd packages/vite-plugin-loca
npm test
```

## Testing the npm package locally

The Vite plugin can be packed and installed without publishing it:

```sh
npm pack ./packages/vite-plugin-loca --dry-run
npm pack ./packages/vite-plugin-loca
npm install --save-dev /path/to/loca/vite-plugin-loca-0.1.0.tgz
```

The dry run should contain only the package README, license, manifest, JavaScript entrypoint, and TypeScript declaration.

## Project layout

- `cmd/loca`: CLI and macOS service integration
- `internal/router`: routing, HTTPS, persistence, and control API
- `packages/vite-plugin-loca`: Vite integration package
- `scripts`: local installation helpers

Keep platform-specific behavior at the edges of the router. The core routing and persistence code should remain testable without a running macOS service.
