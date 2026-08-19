---
name: data-backup
description: Automatically backup the working directory with git stash before any modifications, preventing data loss from agent operations. Use at the start of every conversation or when the user mentions backup, data loss prevention, snapshot, or safe mode.
---

# Data Backup

## Core Principle

Before making any file modifications in a conversation, the working directory must be backed up via `git stash` to create a recoverable snapshot. This prevents irreversible data loss from agent operations.

## Pre-execution Backup Workflow

At the **start of every conversation**, before any file reads, writes, edits, or tool invocations that could modify files:

1. Detect the workspace root directory (the git repository root)
2. Check if it's a git repository: verify `.git` exists
3. Check for any uncommitted changes (modified, staged, or untracked files):
   ```bash
   git status --porcelain
   ```
5. If there are uncommitted changes, determine the round number for this session:
   - Count existing stashes matching the current `session_id`:
     ```bash
     git stash list | grep -c "[<session_id>]"
     ```
   - Round number = count + 1
6. Construct the stash message:
   - Extract a short topic from the user's first message (e.g. "sqlite3-step not defined")
   - Use the project name and session_id
   - Combine into the format below
7. Run the stash command:
   ```bash
   git stash push --include-untracked -m "<YYYYMMDD_HH:MM:SS> [<session_id>] #<round> <topic>"
   ```
   - Timestamp is in **local time**
   - `[<session_id>]` is the current AI session identifier (e.g. conversation ID or round ID)
   - `#<round>` is the per-turn counter within this session (1, 2, 3...)
   - `<topic>` is a short quote or paraphrase of the user's request (e.g. "sqlite3-step not defined", "add validate tool", "update hash docs")
   - This stashes all modified, staged, and untracked files
   - The working directory is restored to a clean HEAD state

8. Confirm the stash was created:
   ```bash
   git stash list
   ```

## Stash Message Format

Must follow this exact format:

```
<YYYYMMDD_HH:MM:SS> [<session_id>] #<round> <topic>
```

- `<YYYYMMDD_HH:MM:SS>` — local timestamp of when the backup was taken
- `[<session_id>]` — AI conversation session identifier (e.g. `[2dda5ef9]`)
- `#<round>` — per-turn counter within this session (1, 2, 3...)
- `<topic>` — a short quote or paraphrase of the user's question/request (e.g. `"sqlite3-step not defined"`, `"add validate tool"`)

### Examples

| User message | Generated stash message |
|---|---|
| "sqlite.no 168-173 'sqlite3-step' is not defined" | `20260706_19:30:00 [2dda5ef9] #1 sqlite3-step not defined` |
| (second turn in same session) | `20260706_19:35:00 [2dda5ef9] #2 add validate tool` |
| "更新 std/overview.md 的 hash 文檔" | `20260707_10:15:30 [2dda5ef9] #1 update hash docs` |

## Recovery Instructions (for user reference)

If the user needs to restore a backup:

| Action | Command |
|--------|---------|
| Apply latest stash (keeps stash in list) | `git stash apply` |
| Apply a specific stash | `git stash apply stash@{n}` |
| List all stashes | `git stash list` |
| View stash contents | `git stash show -p stash@{n}` |

> **Important:** Only use `git stash apply`, never `git stash pop`. Multiple agents may operate concurrently, and `pop` removes the stash, causing data loss for other agents. `apply` leaves the stash in the list for all agents to use. When reverting destructive changes (e.g. after a checkout), use `git stash apply` to restore files.

## Important Notes

- **Only backup at conversation start**, not after every edit
- If `git status --porcelain` is empty (clean working tree), skip the backup — no need to create empty stashes
- `--include-untracked` ensures new files are also backed up
- The stash preserves files even if the agent's operations corrupt or delete them
- **Do not commit changes** — only stash. Committing is a user decision
