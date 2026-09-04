---
name: stage-explicit-paths-parallel-sessions
description: "The user runs parallel Claude sessions on the same repo — never `git add -A`/`-u`, stage the exact paths you edited"
metadata: 
  node_type: memory
  type: feedback
  modified: 2026-08-26T12:14:40.776Z
---

The user often has **another session working the same repo at the same time** (confirmed 2026-08-26 on hexarena, where they also keep git worktrees under `.claude/worktrees/` for it). So the working tree contains edits that are not yours.

**Why:** `git add -A` sweeps them into your commit. It happened twice in one session: three unrelated SVGs rode into a schema-fix PR, and an in-progress `ember` rename rode into an i18n PR — landing on `main` inside a commit whose message says nothing about them. The user's reaction both times was "ko sao", but the commit history is now wrong about who changed what, and a half-finished edit can land mid-thought.

**How to apply:** stage the exact paths you edited — `git add path/a path/b` — and read `git status --short` before committing. If an unexpected path shows up, say so and leave it alone rather than deciding it looks harmless. Same reason to verify what actually got staged: [[verify-committer-staged-files]].

Also: `git worktree list` before touching branches; a locked worktree is the other session's. Don't prune, remove, or rebase what you did not create.
