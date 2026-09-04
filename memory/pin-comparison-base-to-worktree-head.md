---
name: pin-comparison-base-to-worktree-head
description: "To prove a branch is behaviour-preserving, build the 'before' binary from the worktree's own HEAD — never from origin/main, which parallel sessions move mid-task"
metadata: 
  node_type: memory
  type: feedback
  modified: 2026-08-28T13:15:54.205Z
---

When proving a change is inert (same output before/after), build the **"before" artifact from the worktree's own `HEAD`** — `git archive HEAD` — not from `origin/main`.

**Why:** parallel sessions merge PRs constantly (see [[stage-explicit-paths-parallel-sessions]]). On 2026-08-28, verifying hexarena's crit PR, I built the base from `origin/main` and got a diverging battle log, then spent four bisection rounds hunting a bug in code that was correct. `origin/main` had moved two commits during the coder's run, and one of them (#134) rewrote skill ranges in the seed data — so the two binaries were fighting different games. Rebuilt from `HEAD` (the worktree's actual base): byte-identical across 12 seeds. The trap is that the wrong base produces a *plausible* failure, not an obvious one.

**How to apply:** `git archive HEAD | tar -x -C <scratch>` then build there. Before reporting any before/after comparison, print `git log --oneline -1 HEAD` and `git rev-list --count HEAD..origin/main` — a non-zero count means your base is not what you think. Then check reproducibility (run the base twice) before concluding the change is the variable.
