---
name: data-backup
description: Prohibits the use of git stash anywhere in the project. No automatic backup is performed during development. Use when the user mentions backup, data loss prevention, snapshot, or safe mode to enforce the no-stash policy.
---

# Data Backup Policy

## Core Principle: git stash is PROHIBITED

**`git stash` is strictly forbidden** in this project — in all locations, at all times, by all agents and tools.

No automatic backup, snapshot, or stashing of any kind is performed before or during file modifications. Development proceeds directly on the working tree without any stash-based safety net.

## Prohibited Commands

The following `git stash` subcommands must **never** be executed:

| Command | Status |
|---------|--------|
| `git stash push` | ❌ Prohibited |
| `git stash pop` | ❌ Prohibited |
| `git stash apply` | ❌ Prohibited |
| `git stash drop` | ❌ Prohibited |
| `git stash list` | ❌ Prohibited |
| `git stash show` | ❌ Prohibited |
| `git stash clear` | ❌ Prohibited |
| `git stash` (any form) | ❌ Prohibited |

## No Backup Workflow

At the start of every conversation and throughout all file modifications:

1. **Do NOT run `git stash`** in any form.
2. **Do NOT create any backup snapshots** — the user has explicitly chosen to work without backups.
3. Proceed directly with file reads, writes, edits, and tool invocations as needed.
4. If you accidentally ran `git stash`, immediately restore with `git stash pop` to put changes back, then never use stash again.

## Rationale

The user has decided that:
- No backup operations are needed during development.
- `git stash` can cause confusion, lost changes, and merge conflicts.
- The user prefers to manage version control state manually (commits, branches, etc.) and does not want AI agents interfering with the git working tree state.

## Important Notes

- This policy applies to the **entire project** and **all agents/tools** operating within it.
- Do not create stash-based backups at conversation start, before edits, or at any other time.
- If the user asks to "backup" or "snapshot" the working directory, **do not use `git stash`**. Instead, inform the user that stash is disabled and suggest alternatives like `git commit` or `git diff > patchfile` if they explicitly request a backup.
- This rule overrides any other skill or instruction that references `git stash`.
