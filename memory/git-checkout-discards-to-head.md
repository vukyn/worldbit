---
name: git-checkout-discards-to-head
description: never undo a temporary mutation with git checkout <path> on a dirty unstaged tree — it discards to HEAD, not to the last good state
metadata:
  type: feedback
---

⚠️ **`git checkout <path>` on a worktree with unstaged work discards to HEAD, not to "before my mutation".** I mutation-tested an agent's change by editing a file, then ran `git checkout <path>` to undo the mutation — and wiped every one of that agent's edits to the file. The tree looked safe *because* nothing was staged, which is exactly what made it unsafe.

**Why:** this workflow makes the trap routine. Agents leave the tree dirty for review, the committer stages explicit paths only at the very end (see [[stage-explicit-paths-parallel-sessions]]), and mutation-checking a claim is how work is verified here — so the file is always unstaged and always modified when the undo happens.

**How to apply:** undo a mutation by **editing the mutated lines back** (Edit tool), or copy the file first, or `git stash`. Never `git checkout`/`git restore` a path to revert a temporary edit. Applies the same to `git checkout .` and `git reset --hard`.

Recovery, if it happens anyway: the subagent that wrote the file still has it in context — resume it with SendMessage and have it re-apply, then diff against what it reported. Cheaper and more faithful than reconstructing from a diff in the transcript.

See [[verify-committer-staged-files]], [[pin-comparison-base-to-worktree-head]].
