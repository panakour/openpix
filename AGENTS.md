# AGENTS.md

Repo-specific guidance for coding agents working on `openpix`.

`README.md` owns the user-facing overview, install steps, and common development commands. This file focuses on behavior, invariants, and change discipline inside the codebase.

## Start Here

- The binary name is `openpix`.
- Bare `openpix` prints help.
- Real execution paths go through the `wikimedia` and `openverse` subcommands.

Before finishing substantial changes, run:

```bash
go test -race ./...
go vet ./...
golangci-lint run
go build ./...
```

Optional release smoke test:

```bash
goreleaser release --snapshot --clean
```

## Architecture

```text
main -> cli -> provider, download, httpx
                provider -> httpx
                download -> provider, httpx
```

`internal/cli/`

- Cobra command tree plus Bubble Tea UI.
- `run()` chooses between TTY UI and headless execution.
- Runtime presentation belongs in the model `View()` or command help/version paths.

`internal/provider/`

- Defines `Image`, `Query`, and `Provider`.
- `Query.Validate()` owns query invariants.
- `SizeBucket` is a width bucket, not file size.
- Built-in provider names use `SourceWikimedia` and `SourceOpenverse`.
- `Width == 0` or `Height == 0` means unknown and should not fail size filters by itself.

`internal/download/`

- `Pipeline.Run` fans out downloads with `errgroup.SetLimit`.
- Atomic writes and idempotent reruns are core behavior.
- Per-image failures should be reported, not escalated into batch failure.
- Only context cancellation should abort the full run.

`internal/httpx/`

- Thin wrapper over `net/http`.
- Retries transient `5xx`, `429`, and network errors.
- Honors `Retry-After`.
- `Client.Do` clones requests before mutating headers.
- There is no client-wide timeout; callers control deadlines through context.

## Change Style

- Prefer small, direct changes over broad refactors.
- Keep behavior explicit and easy to trace.
- Wrap errors with short context: `fmt.Errorf("context: %w", err)`.
- Put user-facing guidance in returned errors, not ad-hoc prints.
- Do not add normal runtime `fmt.Println` calls.
- `// nolint: exhaustruct` is expected on Cobra command literals with many optional fields.
- Prefer `httptest` for provider and HTTP behavior tests.

## Adding a Provider

1. Add `internal/provider/<name>.go` with `New<Name>(*httpx.Client)` and `Search(context.Context, Query) ([]Image, error)`.
2. Return `provider.Image{Source: provider.Source..., ...}` values.
3. Mirror the existing `httptest`-based provider tests.
4. Add a Cobra subcommand in `internal/cli/root.go` and wire it into `root.AddCommand(...)`.

Example:

```go
func (a *app) fooCmd() *cobra.Command {
	return &cobra.Command{
		Use: "foo",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.run(cmd.Context(), provider.NewFoo(a.http))
		},
	}
}
```

## Docs

- Update `README.md` when user-visible behavior, install steps, or development workflow changes.
- Update this file when architectural rules or repository-specific conventions change.
