# Memory

Distilled, one-fact-per-file notes about this repository — the layer between a
commit message and the design docs. Each file holds **one** thing that was hard
to learn, why it matters, and how to apply it; the index below is one line per
file and nothing else.

This exists **in the repository** rather than in a machine's own Claude memory
directory because that directory is workspace-scoped and machine-local: opening
this repo on another machine, or outside the workspace it was written in, arrived
with none of it.

**Rules, and they are the rules the notes were written under:**

- **One line per note in the index: a link and a hook.** Detail lives in the
  file, never here — an index that grows prose stops being skimmable, which is
  the only thing an index is for.
- **One fact per file.** A note that has to say "and also" is two notes.
- **Say why, not just what.** A rule with no reason gets "simplified" away by the
  next reader; the ones marked ⚠️ are the traps that already cost a session.
- **A `[[link]]` with no file here is fine** — it marks a note worth writing, or
  one that belongs to another repository on the same platform.
- **Delete a note that turns out to be wrong** rather than adding a second note
  that contradicts it.

⚠️ **This is a distillation, not the record.** The repository's own documents
stay the authority; where a note and one of them disagree, the file that owns the
subject wins and the note is the thing to fix.

## This repository

- [worldbit plan](memory/worldbit-plan.md) — deterministic world sim; sim pkg bans maps/floats/time/rand; P0-P3 SHIPPED

## General — platform habits and language traps that apply here

These are not about this repository — they are platform-wide habits and
language traps that apply **while working in it**, copied here so the notes above
read whole on a machine that has only this repo. The full platform set lives in
the workspace they were written in; this is the part that reaches here.

- [Comment style generic](memory/comment-style-generic.md) — comments high-level, no specific usecase detail
- [Commits always via PR](memory/commits-always-via-pr.md) — never direct push to main
- [git checkout discards to HEAD](memory/git-checkout-discards-to-head.md) — never revert a mutation with checkout; edit it back
- [Mutate the producer](memory/mutate-the-producer-not-just-the-logic.md) — grep every write site; compile-failing mutations prove nothing
- [Pin base to worktree HEAD](memory/pin-comparison-base-to-worktree-head.md) — before/after proof: archive HEAD, not origin/main
- [Stage explicit paths](memory/stage-explicit-paths-parallel-sessions.md) — parallel sessions on same repo; never `git add -A`
- [Verify committer staged files](memory/verify-committer-staged-files.md) — committer misreported 2×; verify show --stat + branch + log
