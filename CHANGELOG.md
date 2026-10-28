# Changelog

All notable changes to the nex-web framework are documented here, newest first.
This file lists **what** changed per version/build — technical detail, root
cause, and verification notes live in `handoff.md` only.

## [0.1.2] - R281026 - 2026-10-28

### Added

- Automatic updates: check once per start, verified download and installation
  with automatic rollback, configured with `NEXWEB_UPDATE_URL` (GitHub
  repository or JSON feed); `updater.*` API and an **Updates** section in the
  template
- Banner in the template offering a reload when the server runs a newer
  version than the open page
- Build script and `make requirements` / `make doctor` check that the garble on
  `PATH` is compatible with the installed Go; an outdated garble stops the build
  early with a message asking to update it

### Changed

- Framework release metadata updated to `0.1.2` / `R281026`
- Release folder name now comes from `NEXWEB_APP_VERSION` / `NEXWEB_APP_BUILD`
  (falls back to the framework metadata when either is missing)
- Start message of the daemon and backend/listening/public log lines show the
  app name instead of "nex-web"
- Template `main.go` loads `.env` before detaching and logs the app and the
  framework identities
- App identity (name, version, build, author) is baked into the executable at
  build time, so a distributed binary starts with its own identity without a
  `.env`; a runtime `.env` still takes precedence

## [0.1.1] - R071026 - 2026-10-07

### Changed

- Go toolchain bumped to 1.27.1
- Frontend: React/React DOM 19.3.0, Vite 8.3.2, `@vitejs/plugin-react` 6.1.2
  (`react-icons` stays at 5.7.0, already latest stable)

### Fixed

- `sys.net.port` now builds the dial address with `net.JoinHostPort`, fixing
  IPv6 hosts (e.g. `::1`) that were previously misformatted and always
  reported as closed

## [0.1.0] - R070726 - 2026-07-07

### Added

- Full audit of all 51 `sys.*` APIs exposed by the framework
- Pure Go rewrite of `sys.os.*` / `sys.proc.list` per platform
  (Linux/Darwin/Windows) — no subprocess calls left
- `linux/arm64` added to the cross-compilation targets

### Fixed

- Zombie process left behind by `sys.shell.start` for background commands
- `sys.fs.unwatch` missing its authorization check
- `sys.http.fetch` not re-authorizing HTTP redirects
- Default origin check rewritten to be loopback-based instead of comparing
  against the `Host` header, fixing authentication failures behind the dev
  proxy
- Project failing to compile on Darwin due to two non-existent `syscall`
  symbols
- Dev-mode startup race where Vite could serve the page before the Go
  backend was listening, causing transient 502 responses
- `res/nexweb.svg` masthead logo misaligned (wordmark offset)

### Removed

- `sys.screen.info` (would have required cgo to implement properly on a
  typically headless web server; removed rather than shipped non-functional)

## [0.1.0] - R060726 - 2026-07-06

### Added

- License button + modal in the showcase UI; `FileModal` generalized from
  the previous `DocsModal`
