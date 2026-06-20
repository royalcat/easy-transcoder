# AGENTS.md

This file provides guidance to AI agents when working with code in this repository.

## Build & Development Commands

```bash
# Regenerate templ Go code AND Tailwind CSS output (run after any .templ or CSS change)
make generate

# Build the binary to build/easy-transcoder (also runs generate)
make bin

# Start hot-reload dev server (uses air, watches .go and .templ files, runs from bin/)
make dev-server

# Multi-platform Docker build (linux/amd64, linux/arm64)
make docker-build
```

Run individual steps:
```bash
go tool templ generate -v                          # templ → Go only
tailwindcss -i ./assets/css/input.css -o ./assets/css/output.css  # CSS only
go build -o build/easy-transcoder ./cmd/easy-transcoder           # Go build only
```

There are no tests or linters configured in this project.

## Architecture

Easy Transcoder is a self-hosted web UI for FFmpeg-based video transcoding. It has a queue-based single-worker processing system with customizable transcoding profiles and quality metric calculations (VMAF, PSNR, SSIM).

**Stack**: Go 1.25 standard library HTTP server, `a-h/templ` for compile-time HTML templates, Tailwind CSS v4, htmx 2.0 for AJAX partial updates, Alpine.js 3.x for client-side state. No JavaScript bundler — all JS is loaded from CDN or minified inline. No database — all task state lives in memory (lost on restart).

### Request flow

1. `cmd/easy-transcoder/main.go` — entry point. Parses `config.yaml`, creates a `Processor`, wires HTTP routes on `:8080` using Go 1.22+ `http.ServeMux` with method+pattern routing (`GET /path`, `POST /path`).
2. All handlers are methods on a `server` struct that holds `Config`, `Processor`, and `logger`.
3. Templates are rendered server-side via `templ.Handler()`; htmx attributes in the HTML drive AJAX requests that return HTML fragments.

### Key packages

| Package | Role |
|---|---|
| `internal/config` | Config loading via koanf: defaults → YAML file → `EASY_TRANSCODER_` env vars |
| `internal/transcoding` | Profile compilation to FFmpeg commands, FFprobe probing, VMAF/PSNR/SSIM calculation |
| `internal/processor` | Task queue engine: task state machine, FFmpeg execution, progress tracking via Unix socket, atomic file replacement |
| `ui/layouts` | Base HTML shell (Tailwind, Alpine, htmx, Font Awesome CDN links) |
| `ui/pages` | Full-page templates: root (queue + create dialog), resolver (compare + quality metrics) |
| `ui/elements` | HTMX fragments: filepicker, fileinfo, queue, status bar |
| `templui/components` | Reusable UI components (button, dialog, selectbox, progress, checkbox, etc.) from the templui library |
| `assets/` | Embedded CSS and JS files served at `/assets/` |

### Task state machine

```
pending → processing → waiting_for_resolution → completed
                           ↓
                      (user rejects) → completed (original kept)
                           ↓
                      cancelled / failed
```

- Tasks are created via `POST /submit/task` (single) or `POST /submit/task-batch` (recursive directory walk, skips files matching profile's batch-exclude codec filter).
- A single worker goroutine drains a buffered channel (capacity 100), calling `processTask()` sequentially.
- During processing, FFmpeg writes progress to a Unix socket; the processor parses `out_time_ms` to compute percentage.
- After transcoding, the task enters `waiting_for_resolution` — the user must compare and choose Replace or Reject.
- File replacement is atomic: write to `.tmp_` file in same directory, then `os.Rename`.
- An auto-reject scanner (30s interval) can automatically reject results larger than the original.

### Configuration

Config merges three layers: hardcoded defaults (2 H264 profiles) → `config.yaml` → `EASY_TRANSCODER_` env vars.

A profile has a `name`, `params` map (passed directly to FFmpeg), and optional `batchexcludefilter` (codec strings to skip during batch operations). Default audio handling is `c:a copy` (passthrough).

### Routes summary

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/` | Main page: queue + create task dialog |
| `GET` | `/resolver?taskid=N` | Resolution page with quality metrics |
| `GET` | `/elements/filepicker?path=&sort=` | HTMX: directory browser (video files only) |
| `GET` | `/elements/fileinfo?path=` | HTMX: FFprobe file details |
| `GET` | `/elements/queue` | HTMX: task queue (auto-refreshes every 2s) |
| `GET` | `/elements/status` | HTMX: FFmpeg version + CPU usage (every 3s) |
| `GET` | `/metrics/vmaf?reference=&distorted=` | Calculate VMAF score |
| `GET` | `/metrics/psnr?reference=&distorted=` | Calculate PSNR score |
| `GET` | `/metrics/ssim?reference=&distorted=` | Calculate SSIM score |
| `POST` | `/submit/task` | Create single task (`filepath`, `profile` form fields) |
| `POST` | `/submit/task-batch` | Batch task creation (recursive dir walk) |
| `POST` | `/submit/resolve` | Resolve task (`taskid`, `replace` bool) |
| `POST` | `/submit/cancel` | Cancel task (`taskid`) |
| `POST` | `/settings/auto-reject-larger` | Toggle auto-reject checkbox |

### Conventions

- Always run `make generate` after editing `.templ` or CSS files.
- The `bin/` directory is the runtime working directory — it contains `config.yaml`, `media/`, and the compiled binary. The `air` dev server runs with `--root ./bin`.
- The `templui` components under `templui/components/` were generated by the templui CLI and should be updated via `go tool templui` rather than hand-edited (check `.templui.json` for config).
- Video file extensions are defined in `internal/transcoding/filter.go` as `VideoExtensions`.
- FFmpeg binary resolution: the processor tries the system `ffmpeg` first; if `CustomFFmpegURL` is set in config, it downloads and extracts a custom binary (supports `.zip`, `.tar.gz`, `.tar.xz`, `.tar.zstd`, `.tar.lzma`).
