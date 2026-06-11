# AGENTS.md

## Overview

Single-file Go Windows tray application that manages `filebrowser.exe` lifecycle. Windows-only — uses registry, system tray, and `syscall.SysProcAttr`.

## Build & Run

```powershell
go run .                                    # dev run
go build -ldflags "-s -w -X main.version=$(git describe --tags --always)" -o dist/FileBrowserGuardian_windows_amd64.exe .  # release build (matches CI)
```

No tests, linter, or typecheck steps exist.

## Architecture

- `main.go` — entire application: config, process management, tray UI, registry autostart
- `icon.ico` — embedded via `//go:embed` at compile time
- `config.json` — runtime-generated, gitignored, resolved relative to executable path

## Key Constraints

- **Windows-only**: `golang.org/x/sys/windows/registry` and `syscall.SysProcAttr{HideWindow: true}` make this non-portable
- **No config in repo**: `config.json` is gitignored; generated on first run with defaults
- **Path resolution**: all paths (exe, config, log) resolve relative to the running executable via `resolvePath()`
- **CI**: GitHub Actions on `windows-latest`, Go 1.26.3; releases trigger on `v*` tags

## Dependencies

- `github.com/getlantern/systray` — system tray icon and menu
- `golang.org/x/sys` — Windows registry access for autostart
