# What CS:CZ ToD Helper does on your computer

This page lists everything the program does outside of its own memory, with the file and function that does it,
so you (or anyone you ask) can check the source. The program has **no dependencies**: `go.mod` has no `require`
line, everything comes from the Go standard library, and there is no C code (no cgo).

## The game

- It **never** reads or writes the game's memory, never attaches to the game's process and never injects anything.
- It never reads any game file except the `CZProfile-*.czp` career profiles in the Save folder you choose.
- The game rules and icons it shows were extracted once, by the developer, with `tools/extract`, and are built into
  the program (`internal/game/data_gen.go`, `internal/web/ui/img/`).

## Network

| What | Where in the code |
|---|---|
| Listens on `127.0.0.1:47634` only (your computer; nothing from the network can reach it). The interface and its API are served there. | `internal/web/server.go`: `Listen` |
| Refuses requests whose `Host` is not `127.0.0.1` or `localhost`, and write requests without its own header (other websites cannot drive it). | `internal/web/server.go`: `guard` |
| At start, asks `http://127.0.0.1:47634/api/ping` whether another copy is already running. | `internal/web/server.go`: `otherInstance` |
| Connects to LiveSplit's TCP server, at the address you set (default `127.0.0.1:16834`), to send `split`. | `internal/autosplit/livesplit.go` |
| Accepts LiveSplit One's WebSocket connection on `/livesplit-one` and sends it `{"command":"split"}`. | `internal/autosplit/livesplitone.go` |

Nothing else: no internet access, no telemetry, no update check, no download.

## Files

| What | Where in the code |
|---|---|
| Creates, rewrites and deletes `CZProfile-<name>.czp` in the Save folder you chose, only when you save, delete or reset a savestate, or create a ToD pack. It sets or clears their read-only flag. It never creates the Save folder. | `internal/savestate/files.go`: `WriteFile`, `Remove` |
| Writes each profile first as `.cscz-tod-helper-CZProfile-<name>.czp.tmp` in the same folder, then renames it to `CZProfile-<name>.czp` in one step, so the game never reads a half-written profile. The temporary file exists for a few milliseconds. | `internal/savestate/files.go`: `WriteFile`, `TempPath` |
| Reads the `CZProfile-*.czp` files of that folder (list, status, import, autosplitter). Import adds them to a bundle and leaves the files as they are. | `internal/savestate/files.go`, `internal/autosplit/watcher.go` |
| Writes its own data in `cscz-tod-helper-data` next to the program (or in your user config folder if that is not writable): `config.json`, `bundles/*.json`, `backups/*.bak` (a profile it was asked to overwrite), `logs/autosplit-*.log` (autosplitter) and `logs/helper-*.log` (every profile it writes or deletes). | `internal/settings`, `internal/bundle`, `internal/web/server.go`: `backup`, `internal/web/activity.go`, `internal/autosplit/watcher.go` |
| At start, creates and deletes an empty temporary file next to the program to see if that folder is writable. | `main.go`: `writable` |

## Other programs

| System | What | Where in the code |
|---|---|---|
| Windows | **None.** The browser is opened with the Windows API `ShellExecuteW`, the folder picker is the Windows API `SHBrowseForFolderW`. | `internal/web/os_windows.go` |
| Linux | `xdg-open` to open the interface in your browser at start; `zenity` or `kdialog` (if installed) when you click **Browse**. | `internal/web/os_other.go` |

## VAC

VAC looks for programs that hook into the game's process, known cheat signatures and modified game files.
This program does none of that: it only edits a save file. Valve does not certify third-party tools,
so nobody can promise it, but nothing here touches what VAC watches.

## Check the program you downloaded

The release files are built by GitHub Actions from the release's commit
([`.github/workflows/release.yml`](.github/workflows/release.yml)), with `build.sh`:

```
GOTOOLCHAIN=go1.24.7 CGO_ENABLED=0 GOARCH=amd64 GOAMD64=v1
go build -trimpath -buildvcs=false -ldflags="-s -w"
```

These builds are reproducible: the same source gives a byte-identical program, whatever computer builds it
(the scripts pin the Go version; Go downloads it by itself if needed). To check a release:

1. Install Go (any recent version, see [Build it yourself](README.md#build-it-yourself)).
2. Build the same version of the source with `build.sh` or `build.bat`.
3. Compare the SHA-256 of your build with the release's (`SHA256SUMS`):
   - Windows: `certutil -hashfile dist\cscz-tod-helper.exe SHA256`
   - Linux: `sha256sum dist/cscz-tod-helper`

Same hash means the downloaded program is exactly this source code.

## Asking an AI to review it

Give it the repository and this file, and ask it to confirm each line above against the code. The places to read
are short: `internal/web/server.go`, `internal/web/activity.go`, `internal/web/import.go`, `internal/web/os_windows.go`, `internal/web/os_other.go`,
`internal/autosplit/`, `internal/savestate/files.go` and `main.go`.

## Reporting a problem

Open an issue on the project's GitHub page.
