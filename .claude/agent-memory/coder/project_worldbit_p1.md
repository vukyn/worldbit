---
name: worldbit-p1-determinism-harness
description: worldbit repo created 2026-07-31 with P0+P1 (skeleton + determinism harness, zero agents); P2-P6 pending, plus the stale-gobuild and seed-independent-golden gotchas
metadata:
  type: project
---

`worldbit/` (module `github.com/vukyn/worldbit`) was scaffolded and P0 + P1 implemented on
2026-07-31: repo skeleton plus the determinism harness, with **no agents at all**. The full
approved design lives in the repo at `worldbit/docs/plan.md`; phases P2 (agents), P3 (stats +
classifier), P4 (batch runner), P5 (ebiten GUI), P6 (polish/docs) are unstarted.

**Why:** determinism is the product feature, not a testing detail — the GUI replays a run by
re-simulating from the seed instead of storing ~1.2 GB of snapshots. So the whole harness
(AST lint, golden fixtures, canonical hash) was built before any gameplay existed.

**How to apply:** when picking up a later phase, read `worldbit/docs/plan.md` first — it is
user-ratified down to individual parameter defaults, and those must not be re-tuned.
Gotchas worth knowing before you start:

- **`gobuild` on PATH is stale (v1.4.0).** It does NOT default the module path to
  `github.com/vukyn/<name>` (it emits a bare `module <name>`), does not emit `LICENSE`, and
  has no `iot` preset — the checked-out `gobuild/` repo is ahead of the installed binary.
  Fix `go.mod` by hand after scaffolding, or rebuild gobuild from source first.
- **`testdata/golden_hashes.csv` is seed-independent until P2.** With zero agents only food
  regrowth runs, and the seed is deliberately not part of the state hash, so all 8 golden
  seeds share one digest per tick. That is expected, not a bug; the eight rows exist so the
  fixture shape is right when agents make the seeds diverge. P2 must regenerate it.
- Regenerating goldens (`make golden`) requires a commit message naming the behavioural
  change that justified it. A golden change riding along with a "pure refactor" is a red flag.
- Cross-arch determinism was confirmed once: `GOARCH=amd64 go test ./internal/sim` under
  Rosetta reproduces the arm64 goldens bit-for-bit.

Related: [[no-artifact-in-pet-platform]], [[kuery-shared-lib-rule]] (worldbit is a standalone
binary like sgo/gobuild/speedtest — the kuery rule and service template do NOT apply).
