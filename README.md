# worldbit

A deterministic agent-based world simulator — "WorldBox-lite" as an **experiment
harness with a viewer attached**, not a game.

The real workflow is headless: run N seeds, classify each run's outcome
(extinct / stable / oscillating / overrun / declining / timeout), emit one
CSV/JSON record per seed, then replay an interesting seed in a GUI **by
re-simulating it from the seed**.

Standalone Go binary — no database, no DI container, no clean-architecture
layers, no `kuery`, no SSO.

## The determinism contract

**Same seed + same config + same binary → the same FNV-1a state hash at every
tick, on every machine and every architecture.**

This is a product feature, not a testing detail: GUI replay works by
re-simulating a run rather than by loading a 1.2 GB snapshot stream, and that
is only sound if the trajectory is reproducible bit for bit. To keep the
guarantee true, `internal/sim` contains **no maps** (iteration order is
randomised), **no floats** (`a*b+c` may be FMA-fused differently on arm64 and
amd64), **no goroutines, channels or `select`**, and **no `time`, `os`, `fmt`,
`log`, `sync`, `math/rand` or `crypto/rand`** — its entire import allowlist is
`math/bits`, `encoding/binary`, `sort`, `errors`. Randomness comes only from
per-agent substreams derived purely from `(seed, agentID, tick, purpose)`, so
adding or removing one agent cannot perturb any other agent's stream. All of
this is enforced mechanically by `TestDeterminismLint`, an AST walker over the
package, plus golden-hash fixtures in `testdata/`.

**No simulation parameter may ever come from an environment variable.** Ambient,
machine-local config is exactly the failure the contract exists to prevent:
same seed on two machines, different result, no visible cause. `.env` carries
only `PRJ` and `VERSION`, which the Makefile consumes.

## Usage

```
worldbit [flags]

--headless, -H          run the batch harness instead of the GUI            (default false)
--seed, -s     uint64   world seed; in batch mode the first of --runs seeds (default 1)
--runs, -n     int      number of consecutive seeds to run (batch only)     (default 1)
--ticks, -t    int      MaxTick; 12000 batch / 120000 deep                  (default 12000)
--out, -o      string   output path; .csv or .json selects the writer       (default "runs.csv")
--workers, -w  int      batch parallelism                                   (default GOMAXPROCS)
--config, -c   string   JSON config file; absent fields keep their defaults (default none)
--set          k=v      single-parameter override, repeatable
--dump-config  string   write the resolved config as JSON and exit
--verify                enable sim invariant assertions (slow)              (default false)
--hash-every   int      print the state hash every N ticks (0 = off)        (default 0)
--scale        int      GUI pixel scale                                     (default 4)
--version, -v
```

Batch seeds are `seed, seed+1, … seed+runs-1`, so any single row of a result
file can be re-run on its own.

### Configuration

Resolution order is **defaults → `--config` file → `--set` overrides**, and
`config_hash` is computed on the *final resolved* config:

```bash
worldbit --dump-config experiment.json          # generate a file to edit
worldbit --headless -c experiment.json --set FoodMax=8 --set InitFoodPerCell=3
```

**Config keys are PascalCase and identical to the `--set` key names** —
`FoodMax`, `InitFoodPerCell`, `MaxTick`, not `food_max`. `--dump-config` prints
the authoritative list. A config file may be partial (absent fields keep their
defaults), but **an unknown key is a hard error**, not a silent no-op: a
mistyped field that was quietly ignored would produce a valid-looking
`config_hash` for parameters nobody chose, and nothing in the recorded output
would reveal it.

`Config.Validate()` runs before a single tick and names the offending field,
because the dangerous invalid values fail *silently and plausibly* rather than
crashing (`MatureAge > MaxAge` makes every run classify EXTINCT and reads
exactly like an ecology bug).

`internal/sim/config.go` is the single source of truth for every parameter;
nothing may hardcode a value that belongs there. Adding a field means adding it
to `Hash()`, to `Validate()` if a rule applies, and giving it a `json` tag —
`TestConfigHashChangesWithEveryField` enforces the first.

### Output schema

One row per seed, fixed column order, LF endings, hashes as `%016x`:

```
seed, outcome, peak_pop, peak_year, extinct_year, final_pop, final_tick, state_hash, config_hash, version, wall_ms
```

`wall_ms` is the one non-deterministic column (capacity planning) — exclude it
from any result diff. **Two records with different `config_hash` are not
comparable**; a diff tool must refuse to compare them.

### Replaying a seed

Run the batch, pick a seed whose outcome looks interesting, then re-simulate it
in the viewer:

```bash
worldbit --seed 4242 --ticks 12000
```

When the viewer reaches the recorded tick it hashes the world and compares
against the `state_hash` from the CSV, so every replay doubles as a live
determinism regression test.

## Make targets

```
make build            # bin/worldbit
make build-headless   # bin/worldbit-headless, built with -tags nogui
make run              # go run . --seed 1
make headless         # a 100-seed batch into runs.csv
make test             # go test ./...
make golden           # regenerate testdata/golden_hashes.csv  (see below)
make bench            # simulation benchmarks
make vet              # go vet ./...
make tag VERSION=x.y.z
```

`-tags nogui` builds a pure stdlib+CLI binary with no graphics stack linked in,
so the batch harness — the primary deliverable — stays buildable on a CI runner
or server with no GL/X11 headers.

### Golden hashes

`testdata/golden_hashes.csv` pins `seed,tick,hash` for eight seeds at ticks
100 / 1 000 / 12 000. `make golden` regenerates it.

Regenerating is a **deliberate act**: it makes a failing test pass, which is
the path of least resistance both when a legitimate change alters behaviour and
when a real determinism bug does. A golden update must come with a commit
message naming the behavioural change that justifies it, and a golden update
riding along with a "pure refactor" is a red flag to investigate, never to
accept.

## Package layout

Dependencies are strictly one-way:

```
main   -> {gui, runner}
gui    -> {sim, stats}
runner -> {sim, stats}
stats  -> {}   stdlib only, no sim import
sim    -> {}   stdlib only
```

- `internal/sim` — the pure, integer-only, single-threaded simulation.
- `internal/config` — file and flag resolution. Lives outside `sim` precisely
  so that `sim` never touches `os`.
- `internal/stats`, `internal/runner`, `internal/gui` — classifier, batch
  harness, viewer (later phases).

## Status

Phase 1: repository skeleton and determinism harness. The world has cells and
food regrowth and is hash-stable and golden-pinned; agents, the outcome
classifier, the batch runner and the GUI are the following phases. `docs/plan.md`
is the full design.
