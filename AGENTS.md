# AGENTS.md

Guidance for AI coding agents (Claude Code, Codex, and compatible tools) working
in or with this repository.

## Running gitrespect for a user

If the user wants to analyze git activity, measure productivity, generate a
report, or compare output before/after a tooling change, follow the bundled
skill: [`.claude/skills/gitrespect/SKILL.md`](.claude/skills/gitrespect/SKILL.md).
It documents the flags, the opt-in `--metrics` set, output formats, and how to
interpret results. Claude Code loads it automatically; other agents should read
it directly.

## Developing gitrespect

- Build: `make build` (or `go build -o gitrespect ./cmd/gitrespect`)
- Test: `make test` (`go test ./...`)
- Lint/format: `make lint`, and run `gofmt -w` before committing
- Architecture and conventions live in [`CLAUDE.md`](CLAUDE.md)
- Commits use Conventional Commits (`feat:`, `fix:`, `docs:`, `refactor:`)
