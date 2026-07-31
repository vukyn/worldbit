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
--sweep        string   JSON parameter-grid file; runs every cell (batch)   (default none)
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

### Parameter sweeps

Seed variation alone produces near-identical runs at a fixed parameter set —
the variety lives in the parameters. `--sweep` runs a grid:

```json
{
  "seeds": 20,
  "seed_start": 1,
  "axes": {
    "InitFoodPerCell": [1, 3, 5],
    "ReproEnergyCost": [30, 40, 50],
    "FoodRegrowTicks": [25, 100, 400, 1600, 6400]
  }
}
```

```bash
worldbit --headless --sweep grid.json --out sweep.csv
```

Axis keys are `Config` field names, resolved through **the same code `--set`
uses**, so an unknown key is a hard error exactly as it is for `--config`. Cells
are the Cartesian product of the axes; each runs `seeds` runs, and the base
config resolves as usual (defaults → `--config` → `--set`) before a cell layers
its axis values on top. `Validate()` then runs **per cell**: an invalid cell is
reported by name and skipped, so one impossible corner of a grid does not throw
away the rest of it.

The total run count and a wall-time estimate are printed **before** the sweep
starts. There are no silent caps in this harness, so knowing what you launched
is the only protection against launching a six-hour sweep by accident.

### Output schema

One row per run, fixed column order, LF endings, hashes as `%016x`:

```
seed, outcome, peak_pop, peak_year, extinct_year, final_pop, final_tick, state_hash, config_hash, version, wall_ms
```

Under `--sweep`, one column per swept axis is appended **after** those eleven,
so a row is self-describing and a tool that knows the canonical schema keeps
working. `config_hash` is always the hash of the config *that row* used, which
in a sweep is the cell's, not the base's.

`wall_ms` is the one non-deterministic column (capacity planning) — exclude it
from any result diff. **Two records with different `config_hash` are not
comparable**; a diff tool must refuse to compare them.

Results are sorted by cell index and then by seed before writing, so two runs of
the same batch at different worker counts produce files that are **identical
apart from `wall_ms`**. They cannot be byte-identical while a wall-clock column
is in the schema — blank that column before comparing two result files.

Two more files are written next to `--out`:

| File | Contents |
|---|---|
| `<out>.config.json` | the resolved base config, so a result set is reproducible on its own |
| `<out>.cells.csv` | (sweeps only) one row per cell: axis values, the count of each of the six outcomes, and mean/median peak and final population |

The aggregate is what answers *which parameter region is interesting*; the
per-run file says what each individual seed did.

**Early stop.** EXTINCT and OVERRUN end a run where they happen and the actual
tick is recorded in `final_tick` — an overrun run costs roughly thirteen times a
normal one per tick, which across a sweep is the difference between minutes and
hours. STABLE is terminal for the classifier but deliberately does **not** stop
the simulation, so `final_pop`, `final_tick` and `state_hash` describe the same
point in time for stable runs as for every other kind and replay verification
keeps working.

### Classifier burn-in

Window 1 of every run contains the startup transient: the founding population is
fed by the initial pantry, booms far past the carrying capacity, and crashes
back. That window can never be stable, and its coefficient of variation would
latch the high-variation flag for the rest of the run — making OSCILLATING the
automatic end-of-run fallback for any run that fails to string together three
stable windows. OSCILLATING would then mean *"did not reach STABLE in time"*
rather than *"genuinely oscillating"*, and would mislabel whole regions of a
sweep.

`BurnInWindows` (default **1**) excludes that many leading windows from **every**
predicate — the stable streak, the high-variation flag and the declining-slope
fallback alike. The windows are still accumulated and still advance the
boundary, and the per-tick EXTINCT and OVERRUN checks still fire during them.
`--set BurnInWindows=0` reproduces the un-burnt-in behaviour exactly.

It is a classification parameter, not a simulation one — it cannot move a single
state hash — but it lives in `Config` so that it travels in `config_hash` (two
runs classified with different burn-ins are not comparable) and so that a sweep
can vary it.

### High-variation population floor

The coefficient of variation is **scale-free**, and the OSCILLATING predicate
compares it against a fixed 0.25. Demographic noise in a population of mean *p*
has a standard deviation of about √*p*, so its coefficient of variation is about
1/√*p*:

| Mean population | Coefficient of variation from noise alone | 0.25 line sits |
|---|---|---|
| 16 | 0.25 | **at** the noise floor — the predicate cannot fail |
| 64 | 0.125 | at twice the noise floor |
| 700 | 0.038 | far above noise |

So without a floor, a starving remnant of a dozen agents is reported as an
oscillating ecology purely because small numbers are noisy — which is exactly
what the P4 sweep found.

`MinOscillatingPopulation` (default **64**) is the mean population a window must
reach before its variation may count. It mirrors `MinStablePopulation`, with one
deliberate difference: it tests the window **mean** rather than the population at
the boundary, because the coefficient of variation is a property of the whole
window. The comparison is the exact integer `S1 >= floor·n` — no division, no new
accumulator. `--set MinOscillatingPopulation=0` reproduces the un-floored
behaviour exactly.

The default is derived rather than chosen: 64 is where the 0.25 line sits at
twice the demographic-noise level, so clearing it requires a population genuinely
twice as variable as chance. Measurement over a 1200-run grid agrees — above a
mean of 40 not one firing window is noise-explainable, while 97 % of windows
below a mean of 8 fire and their variation is indistinguishable from pure noise.

Like `BurnInWindows`, it is a classification parameter that cannot move a state
hash, and lives in `Config` so it travels in `config_hash` and can be swept.

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
- `internal/config` — file, flag and sweep resolution. Lives outside `sim`
  precisely so that `sim` never touches `os`, and outside `runner` so that the
  one-way graph above holds.
- `internal/stats`, `internal/runner`, `internal/gui` — classifier, batch
  harness, viewer (the last of these is a later phase).

Parallelism exists in `internal/runner` and only there, and only **between**
runs: each worker builds its own `World` and shares no simulation state.
`TestRunnerParallelMatchesSerial` proves that one worker and eight produce
identical files once `wall_ms` is blanked.

## Status

Phases 1–4 are done: the determinism harness, agents, the outcome classifier
and the batch runner with parameter sweeps. The GUI (phase 5) is still a stub —
`go run . --seed 1` reports that the viewer has not landed. `docs/plan.md` is
the full design.

### What the parameters actually do

Mapped by a 45-cell sweep over `InitFoodPerCell` × `ReproEnergyCost` ×
`FoodRegrowTicks`, 20 seeds per cell. `FoodRegrowTicks` dominates, because it
sets the carrying capacity (`cells / FoodRegrowTicks × EnergyPerFood /
BurnPerTick`); the other two mostly move the height of the opening boom:

| `FoodRegrowTicks` | Capacity | Outcome |
|---|---|---|
| 25 | ~6550 | OVERRUN, every seed |
| 100 | ~1640 | STABLE, every seed |
| 400 | ~410 | STABLE / TIMEOUT mix, a few DECLINING |
| 1600 | ~102 | TIMEOUT, a few DECLINING |
| 6400 | ~26 | TIMEOUT, a few DECLINING (was OSCILLATING before the population floor) |

EXTINCT needs a metabolic squeeze rather than a food one: `BurnPerTick=5`
extinguishes every seed at `FoodRegrowTicks` ≥ 1600.

**Genuine oscillation lives at high burn and moderate regrowth**, a region the
first sweep did not cover: `BurnPerTick=5` with `FoodRegrowTicks` 300–800
produces a real limit cycle, 20/20 seeds, with the population swinging roughly
between a third and double the carrying capacity about a stationary mean, window
after window for the whole run. At `FoodRegrowTicks=400` that is a swing of about
30–180 around a mean of 82. These survive the population floor; the starving
remnants at slow regrowth do not.

The **defaults sit at `FoodRegrowTicks=200`**, between the all-STABLE and mixed
bands: a 1000-seed batch there is 966 STABLE, 33 TIMEOUT, 1 DECLINING, and
EXTINCT / OVERRUN / OSCILLATING are unreachable. That is a deliberately quiet
region, which is what makes it a good baseline — but a sweep, not a seed batch,
is the way to exercise the classifier.
