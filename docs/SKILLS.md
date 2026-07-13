# Agent skill bindings

`shenron.yaml` is the source of truth for skill metadata. `shenron push <name>`
emits each binding to the **Claude Code** agent frontmatter and adds it to the
**Codex** agent's instruction prompt.

> **OpenCode output does not contain a `skills` key on generated agents.**
> OpenCode v1.x does not recognize `skills` as an agent field, so the CLI
> forwards unknown top-level options to the LLM provider as payload fields.
> Strict providers (Pydantic `additionalProperties: false`, e.g. GLM-5.2)
> reject them with HTTP 400 `Extra inputs are not permitted`. Shenron therefore
> drops the field for OpenCode; OpenCode agents are expected to reference
> skills from their prompt body (e.g. an `## Available skills` section).

| agent | skills | file |
|---|---|---|
| `ask` | `using-superpowers`, `brainstorming`, `codebase-design`, `domain-modeling`, `ubiquitous-language` | `~/.claude/agents/ask.md` |
| `build` | `test-driven-development`, `verification-before-completion`, `dispatching-parallel-agents`, `setup-pre-commit`, `go-cli-conventions`, `schema-validation` | `~/.claude/agents/build.md` |
| `debug` | `systematic-debugging`, `verification-before-completion` | `~/.claude/agents/debug.md` |
| `git` | `git-guardrails-claude-code`, `resolving-merge-conflicts`, `finishing-a-development-branch` | `~/.claude/agents/git.md` |
| `plan` | `writing-plans`, `design-an-interface`, `codebase-design`, `to-issues`, `decision-mapping`, `go-cli-conventions`, `schema-validation` | `~/.claude/agents/plan.md` |
| `salameche` | `verification-before-completion`, `requesting-code-review` | `~/.claude/agents/salameche.md` |
| `carapuce` | `verification-before-completion`, `requesting-code-review` | `~/.claude/agents/carapuce.md` |
| `bulbizarre` | `verification-before-completion`, `requesting-code-review` | `~/.claude/agents/bulbizarre.md` |
| `orchestrator` | `using-superpowers`, `verification-before-completion`, `requesting-code-review`, `dispatching-parallel-agents`, `finishing-a-development-branch` | `~/.claude/agents/orchestrator.md` |

The current dogfood pivot contains nine agents. It has no `docs` agent, so no
binding is invented for one. Agents without critical skills may omit `skills`.

## Missing skills to write

All skills identified by the wiring plan are now written under
`~/.agents/skills/`.

| skill | status |
|---|---|
| `go-cli-conventions` | written |
| `schema-validation` | written |
| `atomic-file-write` | written |
| `golden-file-testing` | written |
| `adapter-pattern` | written |
| `binary-distribution` | written |
| `embedded-fixtures` | written |
