<h1 align="center">Bulk Image Downloader</h1>

<p align="center"><em>Open-licensed images from Wikimedia Commons and Openverse.</em></p>

<p align="center">
  <a href="https://github.com/panakour/openpix/actions/workflows/test.yaml"><img alt="verify" src="https://github.com/panakour/openpix/actions/workflows/test.yaml/badge.svg"></a>
  <a href="https://github.com/panakour/openpix/actions/workflows/lint.yaml"><img alt="lint" src="https://github.com/panakour/openpix/actions/workflows/lint.yaml/badge.svg"></a>
  <a href="https://goreportcard.com/report/github.com/panakour/openpix"><img alt="report" src="https://goreportcard.com/badge/github.com/panakour/openpix"></a>
  <a href="./LICENSE"><img alt="license" src="https://img.shields.io/badge/license-MIT-blue.svg"></a>
</p>

`openpix` is a small Go CLI for downloading open-licensed images from Wikimedia Commons and Openverse.

It works without API keys, saves to `~/Pictures/openpix` by default, and is meant to feel simple: run a command, get images.

- No sign-up or API keys
- Good for wallpapers, moodboards, demos, and quick image gathering
- Works nicely both interactively and in scripts

## Install

```bash
go install github.com/panakour/openpix@latest
```

Or download a prebuilt binary from [GitHub Releases](https://github.com/panakour/openpix/releases).

Release builds target:

- macOS (`arm64`)
- Linux (`amd64`)
- Windows (`amd64`)

## Quick Start

```bash
openpix wikimedia                            # random Featured Pictures
openpix wikimedia -q aurora -n 10           # search Wikimedia Commons
openpix openverse -q forest                 # search Openverse
openpix openverse -q dog --size small       # small images
openpix openverse -q art -l cc0,by-sa       # filter by license
openpix openverse -q mountains --silent     # script-friendly output
```

Run `openpix --help` or `openpix <command> --help` for the full command and flag list.

`openpix` itself prints help. Real work happens through the `wikimedia` and `openverse` subcommands.

## Where Images Come From

`wikimedia`

- With no query, it samples Featured Pictures from Wikimedia Commons.
- With `--query`, it performs a full-text Commons search for bitmap images.

`openverse`

- Uses the Openverse image API.
- Supports `--license` filtering.
- Supports `--size` buckets: `small`, `medium`, `large`.

Shared filters

- `--min-width` and `--min-height` work across providers.
- Unknown image dimensions are treated as unknown, not automatic failures.

## What You Get

- Safe downloads: files only appear in their final name once the download is complete.
- Friendly reruns: existing non-empty files are skipped instead of downloaded again.
- Sensible network behavior: transient failures retry and respect `Retry-After`.
- Good terminal UX: interactive TTYs get the Bubble Tea UI, while pipes, CI, and `--silent` stay plain.
- Best-effort batches: one bad image does not kill the whole run.

## For Developers

Requirements

- Go `1.26+`
- `golangci-lint`
- `goreleaser` only if you want to test release packaging locally

Useful local runs

```bash
go run . --help
go run . wikimedia -q aurora -n 3
go run . openverse -q forest -n 3
```

Checks

```bash
go test -race ./...
go vet ./...
golangci-lint run
go build ./...
```

Optional local release smoke test

```bash
goreleaser release --snapshot --clean
```

## Project Layout

```text
main.go              program entrypoint
internal/cli/        cobra commands and Bubble Tea UI
internal/provider/   provider interface plus Wikimedia and Openverse
internal/download/   concurrent download pipeline and filename generation
internal/httpx/      retrying HTTP client wrapper
```

## Contributing

Small, direct changes fit this project best. Prefer explicit behavior, standard library patterns, and tests that exercise real behavior through `httptest`.

If you are contributing as a coding agent or want the repo-specific engineering notes, see [AGENTS.md](./AGENTS.md).

## Licensing Note

`openpix` is MIT-licensed, but downloaded images still carry their own source licenses and attribution requirements. The tool targets open-license sources, but you should still inspect metadata before redistribution or reuse.

## License

[MIT](./LICENSE)
