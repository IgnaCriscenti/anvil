# Agent skills for anvil

Skills derived from `SPEC.md`. Each folder has a `SKILL.md` with frontmatter (`name`, `description`)
compatible with Claude Code and other agents that read the Agent Skills format.

| Skill               | When to use it                                                                |
|---------------------|-------------------------------------------------------------------------------|
| `anvil-conventions` | Always at the start: packages, appErrors, local GORM, keyring, §12 debt       |
| `go-gui`            | Any change in `ui/`; includes the official go-gui v0.92 docs in `references/` |
| `sql-drivers`       | `internal/drivers`: Open/DSN, metadata, quoting, SplitStatements, read-only   |
| `query-execution`   | SQL editor, pagination (with the FK filter), history, export                  |
| `grid-editing`      | Data tab editing: selection, edit bar, date editor, pending changes, DML, Save |
| `release`           | CI, GoReleaser, buildapp, AUR/Homebrew/winget                                 |

To have Claude Code load them automatically: `ln -s ../agents/skills .claude/skills`.
