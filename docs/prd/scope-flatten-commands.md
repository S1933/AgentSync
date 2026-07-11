# Scope — flatten the CLI to package-driven top-level commands

## Problem statement

Shenron's `init` / `validate` / `diff` / `push` top-level commands used to
operate on a single `shenron.yaml` discovered from the current directory. The
configuration-package feature was added on top of that, exposed as a
`shenron package <subcommand>` group with its own `install` / `list` /
`update` / `diff` / `push`. Both surfaces existed briefly and the README
still describes both, but the package flow is the only one that gives us
reproducibility, permission approval, and shared distribution. The legacy
top-level commands were removed from `main.go` (commit 364c1da) but their
behaviour, error semantics, and state-file placement are no longer
documented anywhere.

This PRD codifies the package-only surface as the only user-facing CLI,
promotes the five package subcommands to top-level (no `package` parent),
and drops `init` and `validate` outright. The engine in
`internal/cli/sync_runtime.go` and the package store in `internal/package/`
are already shaped for this and stay untouched.

## Goals

1. Every CLI command is driven by the package store at
   `~/.shenron/packages/<name>/<digest>/`. There is no path that operates on
   a bare `shenron.yaml` from the working directory.
2. The CLI surface is five top-level commands: `install`, `list`, `update`,
   `diff`, `push`. No `package` subcommand group.
3. `init` and `validate` are removed. Bootstrap is "hand-write or import a
   package directory, then `shenron install`". Validation is implicit on
   install / update / push and is the responsibility of the package engine.
4. `diff` and `push` always require a `<name>` argument. With no argument
   they print usage and exit non-zero — they never fall back to "the pivot
   in the current directory".
5. `--store <path>` is a root-level persistent flag. Default is
   `~/.shenron/packages`. Every subcommand reads it from the same place.
6. The existing exit codes, error types, and message formats from the
   package flow are preserved byte-for-byte. Users who scripted against
   `shenron package install` see the same output from `shenron install`,
   minus the `package` prefix in usage.

## Non-goals

- Engine refactor. `internal/cli/sync_runtime.go`, `internal/cli/sync.go`,
  `internal/cli/registry.go`, `internal/cli/errors.go`, and
  `internal/package/` stay as-is. Library entry points (`RunPush`,
  `RunDiff`, `RunPackage*`) keep their signatures.
- A migration command for users with a bare `shenron.yaml` and a
  repo-local `.shenron-state.json`. The v1 single-pivot flow is gone; those
  repos are out of scope and the user is expected to author or import a
  package.
- Re-introducing `init` in any form (no "scaffold a package", no "import
  native config into a new local package").
- Re-introducing `validate` as a top-level command. The package store
  already validates on install, update, and push; a separate
  `shenron validate <name>` adds no value.
- Changes to the `shenron-package.yaml` schema, the package store layout,
  permission approval, skill resolution, or the adapter registry.
- A `package` parent command. Even empty, it would shadow the top-level
  intent.

## Final CLI surface

```
shenron [--store <path>] <command> ...

Commands:
  install <source>     Install a local directory or a public HTTPS Git package.
  list                 List installed packages, ordered by name.
  update <name>        Validate and replace an installed snapshot from a new source or ref.
  diff <name>          Show a package's native diff plus its permission grants and missing skills.
  push <name>          Generate and atomically write a package's native files, then update its state.
```

Root persistent flag:

| Flag | Default | Notes |
|---|---|---|
| `--store <path>` | `~/.shenron/packages` | Cache directory for installed packages. Read by every subcommand. |

Per-command flags:

| Command | Flag | Default | Notes |
|---|---|---|---|
| `install` | `--ref <tag-or-sha>` | none | Required when `<source>` is an HTTPS Git URL. Branches and `HEAD` are refused. |
| `update` | `--source <dir-or-url>` | reuse installed | Replaces the source of an installed package. |
| `update` | `--ref <tag-or-sha>` | reuse installed | Pins the new Git revision for HTTPS sources. |
| `diff` | `--target <adapter>` | all | One of `claude-code`, `codex`, `opencode`. |
| `push` | `--target <adapter>` | all | One of `claude-code`, `codex`, `opencode`. |
| `push` | `--force` | `false` | Overwrite manually edited package-owned files. |
| `push` | `--allow-permissions` | `false` | Approve the package revision's declared permission grants; bound to revision + permission digest. |

Argument validation:

| Command | Arity | On violation |
|---|---|---|
| `install <source>` | `ExactArgs(1)` | Cobra prints usage, exit 2. |
| `list` | `NoArgs` | Cobra prints usage, exit 2. |
| `update <name>` | `ExactArgs(1)` | Same. |
| `diff <name>` | `ExactArgs(1)` | Same. |
| `push <name>` | `ExactArgs(1)` | Same. |

## Functional requirements

### FR1 — `shenron install <source>`

- `<source>` is a local directory or a public HTTPS Git URL.
- HTTPS sources require `--ref` set to an immutable tag or full commit
  SHA. Branches, `HEAD`, SSH, and archive URLs are refused.
- The package store copies the source into
  `~/.shenron/packages/<name>/<digest>/`, validates the
  `shenron-package.yaml` manifest, validates the embedded `shenron.yaml`
  pivot, and writes the installed record.
- On success: prints `installed package <name>@<version> (<revision>)` on
  stdout, exits 0.
- On failure: the store is unchanged, an error is returned, exit non-zero.
  No partial package directory is left behind.

### FR2 — `shenron list`

- No args.
- Tab-separated columns: `name`, `version`, `source`, `revision`, one row
  per installed package, ordered by name.
- When no packages are installed: prints `No packages installed`, exits 0.
- Never errors on a clean store. Errors only on store I/O failure.

### FR3 — `shenron update <name>`

- `<name>` is the name of an installed package. Errors with
  `package "<name>" is not installed` if absent.
- `--source` and `--ref` are both optional. When omitted, the installed
  value is reused.
- Stages and validates the new snapshot before swapping the active
  record. Old snapshots are retained.
- On success: prints `updated package <name>@<version> (<revision>)` on
  stdout, exits 0.
- On validation failure: no swap, exit non-zero, error message
  propagates from the pivot / manifest validator.

### FR4 — `shenron diff <name>`

- `<name>` is the name of an installed package. Errors if absent.
- Resolves the package's pivot at
  `~/.shenron/packages/<name>/<active-digest>/shenron.yaml` and runs the
  diff engine with state loaded from
  `~/.shenron/state/<name>/.shenron-state.json`.
- Reports created / modified / forced / orphaned native files for the
  pivot, the same way the package flow does today.
- Surfaces the package's declared permission grants and any missing
  required or optional skills.
- `--target <adapter>` limits the run to one adapter
  (`claude-code`, `codex`, `opencode`). Without it, all adapters are run.
- Exits 0 on a successful run, including the "no changes" case. Exits
  non-zero on engine error, missing package, or unknown target.

### FR5 — `shenron push <name>`

- `<name>` is the name of an installed package. Errors if absent.
- Resolves the package's pivot and state the same way `diff` does.
- Blocks on missing required skills and returns `ErrPackageSkills`. Warns
  on missing optional skills and continues.
- If the revision declares permission grants and the
  `(revision, permissionDigest)` tuple is not yet approved, returns
  `ErrPackagePermissions` unless `--allow-permissions` is passed. The
  approval is persisted at
  `~/.shenron/state/<name>/permissions.json` and is bound to the
  installed revision and the SHA-256 digest of the normalized grant list.
- Returns `ErrPackageCollision` if a generated path (or a managed nested
  entry inside `opencode.json`) exists on disk without being tracked in
  the package's own state file.
- Returns `ErrManualEdits` on manually-edited package-owned files,
  unless `--force` is passed.
- On success: writes files atomically, updates
  `~/.shenron/state/<name>/.shenron-state.json`, prints
  `[<adapter>] wrote <path> (<status>)` per changed file. Prints
  `state updated: <path>` and exits 0.
- Exits non-zero on any of the above error conditions, with the error
  message preserved from the package flow.

### FR6 — root `--store`

- Persistent flag on the root Cobra command, shared by every
  subcommand.
- Default: `$HOME/.shenron/packages` when `--store` is empty.
- A non-existent path is fine for `install`; the package store creates
  it. A path the user cannot read or write is a clear error, not a
  silent fallback.

### FR7 — argument validation

- `install`, `update`, `diff`, `push` use `cobra.ExactArgs(1)`.
- `list` uses `cobra.NoArgs`.
- All subcommands set `SilenceUsage: true` and `SilenceErrors: true` so
  validation failures print the error message but not the full usage
  banner; the root command wraps the returned error and exits with the
  error's exit code (Cobra default: 1).

## Constraints

- The engine in `internal/cli/sync_runtime.go` and `internal/cli/sync.go`
  is not modified. The package flow continues to call
  `runDiffAt` / `runPushAt` with a `stateDir` set to
  `~/.shenron/state/<name>/`.
- `internal/cli/sync_test.go` and `internal/integration_test.go` keep
  using `cli.RunPush` / `cli.RunDiff` as library functions. Neither file
  is touched.
- `internal/cli/package_test.go` keeps testing the library entry points
  (`RunPackageInstall`, `RunPackageList`, `RunPackageUpdate`,
  `RunPackageDiff`, `RunPackagePush`). It is not touched.
- Error types `ErrPackagePermissions`, `ErrPackageSkills`,
  `ErrPackageCollision`, `ErrManualEdits` keep their definitions and
  sentinel values.
- No new third-party dependency.

## File-level changes

### Modified

- `cmd/shenron/main.go` — register five top-level commands on the root
  via `cli.NewRootCmd()`; add the root persistent `--store` flag; no
  call to `NewPackageCmd()`.
- `internal/cli/package.go` — drop `NewPackageCmd` and its
  `cmd.PersistentFlags().StringVar(&storeRoot, "store", ...)` block.
  Expose five constructors: `NewInstallCmd`, `NewListCmd`,
  `NewUpdateCmd`, `NewDiffCmd`, `NewPushCmd`. Each takes a
  `func() *shenronpackage.Store` (built from `--store`) so the root
  flag is read once per invocation. Add `NewRootCmd` that wires the
  root, the five commands, and the persistent `--store` flag.
- `internal/cli/package_apply.go` — split `newPackageApplyCmds` into
  `NewDiffCmd` and `NewPushCmd` constructors that read `--store` from
  the cobra command hierarchy (`cobraCmd.Root().PersistentFlags()`).
- `README.md`:
  - "Quick start" — replace the `init` / `validate` / `diff` / `push`
    sequence with `install` / `list` / `diff` / `push` against an
    installed package. Drop the "To work with one target only"
    section's `init` references.
  - "Commands and flags" — replace the table. New top-level rows are
    exactly `install`, `list`, `update`, `diff`, `push`. Drop the
    `package` group. Move `--store` to a "Common flags" row at the
    root level.
  - "Bootstrap and round-trip behavior" — rename to "Getting started"
    and rewrite around `shenron install ./local-package-dir`.
  - "Configuration packages" — drop the `./shenron package …` prefix
    from every example.
  - "Architecture for contributors" — update the `internal/cli/` tree
    comment to drop `init`, `validate`, `diff`, `push` as user-facing
    commands.
  - "Testing and development" — drop `init` / `validate` mentions
    from the test-suite summary.
- `docs/prd/shenron.md` — add a note in the Decisions log: "D10 — the
  v1 single-pivot flow (`init` / `validate` / `diff` / `push` on a
  bare `shenron.yaml`) was removed in favour of the package flow.
  See `docs/prd/scope-flatten-commands.md` for the current CLI
  contract." Mark FR5 / FR6 / FR7 / FR8 as superseded by the package
  flow rather than deleting them — the schema they describe is still
  what the package flow consumes.
- `docs/ARCHITECTURE.md` — update any text that mentions `init`,
  `validate`, `diff`, `push` as top-level user-facing commands to
  point at the package flow.

### New

- `docs/prd/scope-flatten-commands.md` — this PRD.
- `internal/cli/commands_test.go` — Cobra-driven tests that:
  - assert the root command advertises exactly the five subcommands,
  - assert `diff` and `push` with no args return a non-zero exit and
    a usage hint,
  - assert `--store` is read on the root and propagates to
    `NewStore` calls in every subcommand,
  - assert `--allow-permissions`, `--force`, `--target`, `--ref`,
    `--source` exist on the right subcommands,
  - assert `SilenceUsage: true` / `SilenceErrors: true` are set so
    the usage banner is not printed on engine errors.

### Deleted

- None. The v1 top-level command files were already removed in commit
  364c1da. The README and PRD are updated, not removed.

## Acceptance criteria

1. `./shenron --help` lists exactly five subcommands: `install`,
   `list`, `update`, `diff`, `push`. No `package` parent.
2. `./shenron diff` and `./shenron push` with no args print a short
   error and exit non-zero. They do not consult any pivot in the
   current directory.
3. `./shenron install ./good && ./shenron list` shows the package
   in TSV form. `./shenron list` with no installs prints
   `No packages installed` and exits 0.
4. `./shenron install ./missing` exits non-zero with a clear error
   and leaves no partial package directory under the store.
5. `./shenron diff acme-reviewers` and
   `./shenron push acme-reviewers --allow-permissions` produce the
   same native files and the same
   `~/.shenron/state/acme-reviewers/.shenron-state.json` content as
   the current `shenron package …` flow, byte-for-byte.
6. `./shenron --store /tmp/cache install ./pkg` writes the package
   under `/tmp/cache`, not `~/.shenron/packages`.
7. `./shenron diff acme-reviewers` with no installed package named
   `acme-reviewers` prints `package "acme-reviewers" is not
   installed` and exits non-zero.
8. `./shenron push acme-reviewers` with a missing required skill
   exits non-zero with `ErrPackageSkills`. With a missing optional
   skill it prints the warning and continues.
9. `./shenron push acme-reviewers` with unapproved permission
   grants exits non-zero with `ErrPackagePermissions`; with
   `--allow-permissions` it persists the approval at
   `~/.shenron/state/acme-reviewers/permissions.json` and exits 0.
10. `make test` is green. `make lint` is green. `make build`
    produces a `./shenron` binary whose `--help` matches the table
    above.
11. README "Commands and flags" table matches the implemented
    surface. Every example uses the new top-level form, never
    `shenron package …`.

## Out-of-scope follow-ups (deferred)

These are intentionally not in this PRD; they belong in their own
tickets.

- A `shenron adopt` migration command for users with a bare
  `shenron.yaml` and a repo-local `.shenron-state.json`. Today they
  have to `shenron install .` and delete the old state file by hand.
- A `shenron validate <name>` command that re-validates an installed
  package's pivot and manifest without pushing. The package engine
  validates on install / update / push, which is enough for now.
- Showing last-push digest, last-push time, and target summary in
  `shenron list`. Today it shows `name / version / source /
  revision` only.
- A short hint when `./shenron` is invoked with no subcommand
  ("run `shenron --help`"). Today the Cobra default is silent.
- A `shenron uninstall <name>` command to remove a package from
  the store. Not in scope; old snapshots are already retained on
  update, so this is a separate ergonomic question.
