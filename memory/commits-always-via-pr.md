---
name: commits-always-via-pr
description: "All commits to main go through a PR — never direct push to main, any repo, any size (user rule 2026-06-07)"
metadata: 
  node_type: memory
  type: feedback
---

User rule (set 2026-06-07 after a housekeeping commit was pushed directly to isme main): every change lands on main via branch + PR, no exceptions — including trivial chores, doc moves, build-asset refreshes. Applies to all platform repos (isme, medioa2, rainy, kuery, memz, …).

**Why:** Review gate + audit trail; the committer agent previously had discretion to push direct when "repo convention allowed", which the user rejected.

**How to apply:** Always instruct the committer agent: branch from main → commit → push → open PR; never grant direct-push discretion. Merge only after verifying staged files ([[verify-committer-staged-files]]) and with user approval unless they pre-authorized merging in the same request. kuery releases too: commit via PR, then tag after merge.
