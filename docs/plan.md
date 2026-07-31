# Plan: `worldbit` — deterministic agent-based world simulator

Module `github.com/vukyn/worldbit`, repo dir `/Users/vuky10/vukyn/repo/pet-platform/worldbit`.

## Goal

Standalone Go binary: a **reproducible experiment harness for emergent population dynamics, with a viewer attached**. Primary workflow is headless — run N seeds in parallel, classify each run's outcome (extinct / stable / oscillating / overrun / declining / timeout), emit one CSV/JSON record per seed, then replay an interesting seed in an ebiten GUI **by re-simulating from the seed**. That replay is only possible because the sim is bit-exact deterministic. Determinism is the product feature, not a testing detail.

Standalone binary in the platform sense (peer of `sgo/`, `gobuild/`, `speedtest/`): no DB, no DI, no domains, no clean-arch layers, no kuery, no SSO, no mprocs/hosts entries.

## Affected

- **New repo only.** No cross-service impact. No dependency on isme/medioa2/rainy/kuery/kuino; nothing depends on it. No migrations anywhere.
- Two doc touchpoints outside the repo: the repo-list bullet in the platform-root `CLAUDE.md`, and `.claude/onboarding/worldbit.json` if created via `/pet-onboard`.
- Knowledge graph does not exist yet — build after P2 (`build_or_update_graph_tool` + `embed_graph_tool`, `repo_root=/Users/vuky10/vukyn/repo/pet-platform/worldbit`), and add `.code-review-graph/` to `.gitignore` (the gobuild `base` preset's gitignore lacks it; `sgo/.gitignore` has it).

---

## P0 — Scaffolding

```bash
cd /Users/vuky10/vukyn/repo/pet-platform
gobuild --name worldbit --preset base --go 1.26
```

`gobuild/main.go` defaults `modulePath` to `github.com/vukyn/<name>`, so `--module` is unnecessary. The `base` preset emits `LICENSE Makefile README.md .env .gitignore go.mod main.go todo` and runs `go mod tidy` + `git init`.

Post-scaffold fixes:

1. `.gitignore` — append `.code-review-graph/`, `runs*.csv`, `out/`. Keep the preset's `.env`, `bin/`, `todo`, `.DS_Store`.
2. `.env` — only `PRJ=` and `VERSION=` (Makefile consumes them, mirroring `sgo/.env`). **Never put simulation parameters in `.env` or in any environment variable** — ambient env-derived config silently breaks the "same config" half of the determinism contract (two machines, same seed, different result, no visible cause). Simulation parameters are configurable, but only through the explicit file + flag path described in "External configuration" below.
3. `Makefile` — replace with the sgo/gobuild shape plus sim targets:

```make
#!make
include ./.env
export $(shell sed 's/=.*//' ./.env)

.PHONY: build build-headless run headless test golden bench vet tag

build:
	@go build -o bin/ .
build-headless:
	@go build -tags nogui -o bin/worldbit-headless .
run:
	@go run . --seed 1
headless:
	@go run . --headless --seed 1 --runs 100 --out runs.csv
test:
	@go test ./...
golden:
	@go test ./internal/sim -run TestGoldenHashes -update
bench:
	@go test ./internal/sim -bench . -benchtime 3x -run '^$$'
vet:
	@go vet ./...
tag:
	@[ -n "$(VERSION)" ] || { echo "Usage: make tag VERSION=x.y.z"; exit 1; }
	git tag -a v$(VERSION) -m "Release version $(VERSION)"
	git push origin v$(VERSION)
```

4. `README.md` — what it is, the determinism contract in one paragraph, CLI usage for both modes, the CSV schema, how to replay a seed.
5. `go.mod` — `go 1.26`; requires `github.com/urfave/cli/v2` (platform convention: sgo + gobuild both use it) and `github.com/hajimehoshi/ebiten/v2`. **Nothing else.** No kuery.

---

## File layout

```
main.go                          # urfave/cli/v2 App: flags, mode dispatch
gui_enabled.go                   # //go:build !nogui  -> runGUI() calls internal/gui
gui_stub.go                      # //go:build nogui   -> runGUI() returns "built without GUI"
internal/sim/                    # PURE. stdlib only.
  config.go                      # Config + DefaultConfig() + Config.Hash()
  world.go                       # World, Cell, Agent, WorldView; NewWorld(seed, cfg)
  rng.go                         # splitmix64, AgentRand, Intn (Lemire, integer-only)
  tick.go                        # Step(w): six ordered phases
  env.go                         # regrowth stride, torus index helpers
  index.go                       # coarse 16x16 block food index (derived, NOT hashed)
  agent.go                       # decide() -> Intent; findNearestFood; stepToward
  intent.go
  hash.go                        # FNV-1a canonical state hash
  invariants.go                  # Verify(w), gated by cfg.Verify
  frame.go                       # RenderInto(*Frame) — the ONLY read path the GUI gets
  determinism_lint_test.go       # AST guard
  determinism_test.go
  invariants_test.go
internal/config/                 # file + flag + sweep resolution; the only package that reads os
  config.go                      # Resolve, ApplyOverrides, SetField, Marshal, Dump
  sweep.go                       # Sweep grid: LoadSweep, Expand, per-cell Validate
internal/stats/
  window.go                      # integer accumulators over a 1200-tick window
  intcmp.go                      # 128-bit a*b vs c*d via math/bits.Mul64
  classify.go                    # Outcome enum + classifier state machine + burn-in
  recorder.go                    # pop history (sparkline) + peak tracking
  classify_test.go  burnin_test.go
internal/runner/
  runner.go                      # worker pool over runs; one World per worker; early stop
  record.go                      # Record + Axis: the CSV/JSON row
  output.go                      # CSV + JSON + per-cell aggregate; sorted before emit
  runner_test.go  output_test.go
internal/gui/                    # //go:build !nogui on every file
  game.go  render.go  sparkline.go  hud.go  input.go  snapshot.go
testdata/
  golden_hashes.csv              # seed,tick,hash — committed regression fixture
```

**Strict one-way dependency** (state in CLAUDE.md, enforce in review):

```
main   -> {config, gui, runner}
gui    -> {sim, stats}
runner -> {sim, stats}
config -> {sim}
stats  -> {}   (stdlib only, no sim import)
sim    -> {}   (stdlib only)
```

`sim` must never import `stats`, `runner`, `gui`, `fmt`, `os`, `time`, `log`, `sync`, `math/rand`, `crypto/rand`, `math`.

`runner` must never import `config`, even though the sweep is run by the runner: sweep files are configuration, so parsing and expanding them belongs on the config side, and `main` maps `config.SweepCell` onto `runner.Cell`. That mapping is a dozen lines and is the price of keeping `runner -> {sim, stats}` true.

`internal/` rather than sgo-style `pkg/`: nothing here is meant to be imported by another repo, and `internal/` makes that a compiler guarantee. Trivially reversible.

---

## Key types

```go
// internal/sim/config.go — single source of truth. Nothing may hardcode these.
type Config struct {
    Width, Height   int32  // 128, 128  (MUST be powers of two — torus masking)
    TicksPerYear    int32  // 120
    MaxTick         int32  // 12_000 batch / 120_000 deep

    FoodRegrowTicks int32  // 200
    FoodMax         int16  // 5
    InitFoodPerCell int16  // 1

    EnergyMax       int16  // 100
    EnergyPerFood   int16  // 10
    BurnPerTick     int16  // 1
    HungerThreshold int16  // 70
    MatureAge       int32  // 240
    BirthCooldown   int32  // 60
    MaxAge          int32  // 3000
    ReproEnergyMin  int16  // 60
    ReproEnergyCost int16  // 40  (parent pays)
    ChildEnergy     int16  // 30  (10 lost as birth overhead)

    InitAgents      int32  // 50
    InitAgentEnergy int16  // 50
    InitAgeSpread   int32  // 240 — initial ages uniform in [0, InitAgeSpread)
    SearchRadius    int32  // 12  — Chebyshev cap on nearest-food search

    // Classification, not simulation: the sim never reads it and it cannot
    // move a state hash. It lives here so it travels in config_hash and so a
    // sweep can address it by field name. 0 disables burn-in. See the
    // classifier section.
    BurnInWindows   int32  // 1

    Verify          bool
}

func (c Config) Hash() uint64  // FNV-1a over a fixed-order binary encoding of every field.
                               // Computed on the FINAL RESOLVED config, written into every record.
func (c Config) Validate() error
```

Every field carries a `json:"..."` tag so the struct is both the in-memory config and the on-disk schema — one definition, no DTO to keep in sync.

---

## External configuration

The user requires these parameters to be tunable from outside the binary. This does **not** weaken the determinism contract, which is *same seed + **same config** + same binary*, provided the config is an explicit, recorded, hashed input. The distinction that matters:

| Source | Allowed | Rationale |
|---|---|---|
| Environment variables / `.env` | **No** | Ambient and machine-local. Same seed on two machines silently diverges with no visible cause. This is precisely the failure the determinism contract exists to prevent. |
| Explicit config file via `--config` | **Yes** | It is an input, it travels with the results, and it is hashed. |
| Per-parameter flag override | **Yes** | Lives in shell history; the run is reproducible from the command line alone. |

**Resolution order** — `DefaultConfig()` → `--config` file → `--set` flag overrides. `config_hash` is computed on the **final resolved** config, never on the file as written:

```go
cfg := DefaultConfig()                   // fully populated struct
if path != "" {
    file, err := os.Open(path)           // in main/, never in internal/sim
    decoder := json.NewDecoder(file)
    decoder.DisallowUnknownFields()      // a mistyped key is a hard error, never a silent no-op
    decoder.Decode(&cfg)                 // absent fields keep their default — no pointer fields needed
}
applyOverrides(&cfg, c.StringSlice("set"))
if err := cfg.Validate(); err != nil { return err }
h := cfg.Hash()
```

Decoding into an already-populated struct is what makes partial config files work without pointer fields or a separate DTO.

**Config keys are the PascalCase `Config` field names, identical to the `--set` key names** (`FoodMax`, `InitFoodPerCell`, `MaxTick` — not `food_max`). The naming is not guessable, so `--dump-config` is the authoritative list and the error message points at it.

**Unknown keys are a hard error** (`DisallowUnknownFields`), which is why the two halves of the file contract need separate tests. A silently-ignored typo is the worst available outcome: `{"FoodMaxx": 99}` would decode to the default config, emit a perfectly valid-looking `config_hash`, and produce a thousand seeds of results for parameters nobody chose, with nothing in the recorded output revealing it — precisely the silent divergence this whole design exists to prevent. It also makes the file path consistent with `--set`, which is strict about unknown keys. `DisallowUnknownFields` only rejects keys with no matching struct field, so "absent fields keep their defaults" is unaffected.

**Placement:** file reading and flag parsing live in `main` (or a small `internal/config` shim), **never** in `internal/sim` — `sim` still imports stdlib only and never touches `os`. `sim` receives a finished `Config` value.

**`Validate()` is mandatory, and is new relative to a compiled-in config.** External config means invalid values will arrive, and the dangerous ones fail *silently and plausibly* rather than crashing:

- `Width`/`Height` not a power of two, or > 256 — breaks torus masking (`& 127`) and the `uint8` agent coordinates. Silent corruption.
- `InitFoodPerCell > FoodMax` — regrowth can never restore the initial condition.
- `ChildEnergy > EnergyMax`, or `ReproEnergyCost > ReproEnergyMin` — parent ends a birth with negative energy.
- `MatureAge > MaxAge` — nothing can ever reproduce, so every run is EXTINCT. Reads exactly like an ecology bug and will waste hours.
- `HungerThreshold > EnergyMax`; `FoodRegrowTicks > MaxTick`; any non-positive value.

Fail loudly at startup with the offending field named, before a single tick runs.

**Recording, so an experiment is reproducible:**
- `--dump-config <path>` writes the resolved config as JSON and exits — the canonical record of a run, and the way to generate a starting file to edit.
- The batch runner writes a sidecar `<out>.config.json` next to the CSV automatically.
- `config_hash` is in every CSV row and in the JSON header. Any result-diff tool refuses to compare records whose `config_hash` differs.

**Format: JSON**, via stdlib `encoding/json`. No new dependency, and it round-trips exactly with `--dump-config`. Cost: no comments in the file, mitigated by descriptive field names and by `--dump-config` being self-generating. TOML (`BurntSushi/toml`) is the alternative if hand-written comments turn out to matter; it is one small dependency and a drop-in change.

**Downstream benefit — built, and moved up into P4.** An explicit config makes *parameter* sweeps possible, not just seed sweeps: a grid over `InitFoodPerCell × ReproEnergyCost`, N seeds per cell, mapping which parameter region yields STABLE. Originally sketched as a natural P7, this was pulled forward because seed variation alone at the ratified defaults produces near-identical runs and leaves four of the six outcomes unreachable — the sweep is what turns the harness from a determinism demonstration into an experiment. See "Parameter sweeps" under P4.

**Tests:** `TestConfigRoundTrip` (dump → load → identical `Hash()`); `TestPartialConfigKeepsDefaults` (asserts every unmentioned field is still exactly its default, guarding against a future over-tightening into "every key required"); `TestConfigRejectsUnknownField` (a bogus key errors out naming the field); `TestValidateRejects` (table-driven over every rule above); `TestConfigHashChangesWithEveryField` (reflectively flip each field, assert the hash moves — catches a field added to `Config` but forgotten in `Hash()`, which would silently make two different configs look identical).

```go
type Cell struct {
    Biome uint8
    Food  int16
}

type Agent struct {
    ID            uint32 // globally unique, monotonic, never reused
    X, Y          uint8  // 0..127 torus
    Energy        int16  // 0..EnergyMax
    Age           int32
    LastBirthTick int32  // sentinel birthTick - BirthCooldown, so newborns are simply immature
    TargetIdx     int32  // cached food target, -1 = none. CANONICAL STATE — hashed.
}

type World struct {
    Cfg    Config
    Seed   uint64
    Tick   int32
    Cells  []Cell   // len = W*H, index = (y<<7)|x
    Agents []Agent  // INVARIANT: strictly ascending by ID, always
    NextID uint32
    // ---- derived / scratch, NOT hashed; rebuilt-and-compared under Verify ----
    blockFood []int32  // 16x16 coarse index: food total per 8x8 block
    intents   []Intent
    births    []Birth
}
```

`TargetIdx` influences behaviour → canonical → hashed. `blockFood` is derived from `Cells` → **not** hashed (hashing derived caches makes goldens brittle to legitimate optimisation); guarded by rebuild-and-compare under `Verify` instead.

---

## Determinism: eight break sources and mitigations

**1. Map iteration order.** `sim` contains **no map types at all**. Checkable with pure `go/ast`, no type info: fail on any `*ast.MapType` and any `make(map[...])` under `internal/sim`. Lookups use dense-id-indexed slices or sorted slices with binary search.

**2. Ambient nondeterminism.** Same AST test fails the build on any import of `math/rand`, `math/rand/v2`, `crypto/rand`, `time`, `os`, `runtime`, `sync`, `sync/atomic`, `log`, `fmt` inside `internal/sim`. Allowlist is exactly `math/bits`, `encoding/binary`, `sort`, `errors` — living in the test, so adding an import is deliberate and reviewable.

**3. Per-agent RNG substreams.** No shared sequential stream. Only entry point is a pure function of four words:

```go
const (
    purposeWander   = 1
    purposeTiebreak = 2
    purposeSpawnPos = 3
    purposeInitAge  = 4
)

func splitmix64(x uint64) uint64 {
    x += 0x9E3779B97F4A7C15
    z := x
    z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
    z = (z ^ (z >> 27)) * 0x94D049BB133111EB
    return z ^ (z >> 31)
}

type Rand struct{ s uint64 }

// PURE: same four inputs -> same stream. Adding/removing an agent cannot
// perturb any other agent's stream.
func AgentRand(worldSeed uint64, agentID uint32, tick int32, purpose uint32) Rand {
    h := splitmix64(worldSeed ^ (uint64(agentID) * 0x9E3779B97F4A7C15))
    h = splitmix64(h ^ (uint64(uint32(tick)) * 0xBF58476D1CE4E5B9))
    h = splitmix64(h ^ (uint64(purpose) * 0x94D049BB133111EB))
    return Rand{s: h}
}

func (r *Rand) Next() uint64 { r.s = splitmix64(r.s); return r.s }
func (r *Rand) Intn(n uint32) uint32 // Lemire unbiased, integer only, bounded rejection
```

Multiple draws in one tick pull successively from the returned `Rand` — still pure, since the start state is a function of `(seed, id, tick, purpose)` only. Distinct `purpose` constants keep unrelated decisions uncorrelated.

**4. Integer / fixed-point only.** No `float32`/`float64` tokens permitted in `internal/sim` — the AST test rejects the identifiers. The classifier genuinely needs variance and slope, so it lives in `internal/stats` and computes them **exactly in integers**. Not tidiness: Go may fuse `a*b+c` into an FMA, which differs between arm64 and amd64 — a float classifier could classify the same run differently on two machines despite identical sim state.

**5. Fixed tick, never wall-clock.** `Step(w)` takes no `dt`. The GUI's speed control sets `ticksPerFrame ∈ {0,1,10,100,max}`; `Update()` calls `Step` that many times. "max" runs `Step` until an 8 ms frame budget is spent — wall-clock influences **how far** the sim has advanced when you look, never **what** state N contains. Say this in CLAUDE.md; it's the one place wall-clock legitimately appears near the sim.

**6. No mutation during iteration.** Implemented as **phase separation with an intent buffer** — semantically identical to "read state N, write state N+1" but avoids a 64 KB memcpy per tick (768 MB per 12 000-tick run). Invariant: *no phase both reads and writes the same layer*. The decide phase receives a `WorldView` exposing only value-returning accessors with no mutating methods, so "decide cannot write" is close to compile-enforced. A literal double buffer is a one-line change to `Step` if preferred; not recommended, same guarantee for 768 MB more copying.

**7. Single-threaded sim.** AST test also fails on `*ast.GoStmt`, `*ast.SelectStmt`, and any channel type inside `internal/sim`. Parallelism exists only in `internal/runner`, **between** runs: each worker owns a `World` it built itself and shares no simulation state at all. As built, there is no results channel either — jobs are enumerated up front and each worker writes into its own slot of a preallocated slice, so there is nothing to reorder and no mutex to contend on; the only shared objects are an atomic job cursor and an atomic stop flag. `TestRunnerParallelMatchesSerial` proves it, and `go test -race ./...` is part of the gate.

**8. Determinism is a test, not a hope.** Three layers, all written in P1 before any agent exists:
- `TestSameSeedSameHash` — same seed twice, step both to `MaxTick`, compare `Hash(w)` every 100 ticks; fail on the first divergent tick and report it.
- `TestGoldenHashes` — `testdata/golden_hashes.csv` holds `seed,tick,hash` for 8 fixed seeds at ticks 100 / 1 000 / 12 000. Regenerated only via `make golden`.
- `TestDeterminismLint` — the AST guard.

**State hash** (`hash.go`), FNV-1a 64 over canonical little-endian encoding, hand-inlined (no `hash.Hash64` interface, no allocation):

```
h = 14695981039346656037
mix(int32 Tick); mix(uint32 NextID); mix(int32 len(Agents))
for each Cell:  mix(uint8 Biome); mix(int16 Food)
for each Agent: mix(ID); mix(X); mix(Y); mix(Energy); mix(Age); mix(LastBirthTick); mix(TargetIdx)
```

~66 KB hashed per call → a **checkpoint** op (every 100 ticks in tests, once at end-of-run in batch), not per-tick; per-tick would add ~0.3 s per run. `--hash-every N` exposes it for debugging.

**Extra guard closing the contract:** every output record carries `config_hash` and `version`. Without it, "same seed, different hash" investigations waste hours on what turns out to be a changed constant. Diff tooling must refuse to compare two CSVs with differing `config_hash`.

---

## Tick loop

```
func Step(w *World):
  # ---- phase 1: environment (writes Cells) ----
  # Stride regrowth: exactly ceil(N/200) cells per tick, each cell regrowing once
  # per 200 ticks. NOT "all cells every 200 ticks" — a synchronised global pulse
  # would create a 200-tick sawtooth artefact in every run.
  for i := int32(w.Tick % Cfg.FoodRegrowTicks); i < len(Cells); i += Cfg.FoodRegrowTicks:
      if Cells[i].Biome != BiomeWater && Cells[i].Food < Cfg.FoodMax:
          Cells[i].Food++ ; blockFood[blockOf(i)]++

  # ---- phase 2: decide (reads only; writes only w.intents) ----
  view := WorldView{w}            # accessors only, no setters
  for k, a := range w.Agents:     # slice, ascending ID
      w.intents[k] = decide(view, a)

  # ---- phase 3: resolve (applies intents in ascending-ID order) ----
  for k, a := range w.Agents:
      switch w.intents[k].Kind:
      case Move:      a.X, a.Y = intent.X, intent.Y ; a.TargetIdx = intent.Target
      case Eat:       idx := cellIdx(a.X, a.Y)
                      if Cells[idx].Food > 0 {          # RE-CHECK: a lower ID may have taken it
                          Cells[idx].Food-- ; blockFood[blockOf(idx)]--
                          a.Energy = min(a.Energy + Cfg.EnergyPerFood, Cfg.EnergyMax)
                      }
                      a.TargetIdx = -1
      case Reproduce: if a.Energy >= Cfg.ReproEnergyMin {
                          a.Energy -= Cfg.ReproEnergyCost
                          a.LastBirthTick = w.Tick
                          w.births = append(w.births, Birth{X: a.X, Y: a.Y})
                      }
      case Idle:

  # ---- phase 4: metabolism ----
  for k := range w.Agents: w.Agents[k].Energy -= Cfg.BurnPerTick ; w.Agents[k].Age++

  # ---- phase 5: death (stable in-place compaction, preserves ascending ID) ----
  keep := w.Agents[:0]
  for _, a := range w.Agents:
      if a.Energy > 0 && a.Age <= Cfg.MaxAge { keep = append(keep, a) }
  w.Agents = keep

  # ---- phase 6: birth (parent order; new IDs largest, so append keeps sorting) ----
  for _, b := range w.births:
      w.Agents = append(w.Agents, Agent{ID: w.NextID, X: b.X, Y: b.Y,
                                        Energy: Cfg.ChildEnergy, Age: 0,
                                        LastBirthTick: -Cfg.BirthCooldown, TargetIdx: -1})
      w.NextID++
  w.births = w.births[:0]

  w.Tick++
  if Cfg.Verify { Verify(w) }
```

**Trap to avoid:** births queued in phase 3 must record the parent's `{X, Y}` captured at that moment, **never a slice index** — phase 5 compaction invalidates indices.

**Agent decision (phase 2), pure:**

```
decide(view, a) Intent:
  mature := a.Age >= Cfg.MatureAge
  offCD  := view.Tick() - a.LastBirthTick >= Cfg.BirthCooldown
  here   := cellIdx(a.X, a.Y)

  if mature && offCD && a.Energy >= Cfg.ReproEnergyMin: return Reproduce{X: a.X, Y: a.Y}
  if a.Energy < Cfg.HungerThreshold && view.Food(here) > 0: return Eat{}

  if a.TargetIdx >= 0 && view.Food(a.TargetIdx) > 0:
      return Move{stepToward(a.X, a.Y, a.TargetIdx), Target: a.TargetIdx}
  if t := findNearestFood(view, a); t >= 0:
      return Move{stepToward(a.X, a.Y, t), Target: t}
  return Move{wanderStep(a), Target: -1}
```

**`findNearestFood` — bounded ring search, block-accelerated.** A naive scan of all 16 384 cells per hungry agent per tick is ~13 M cell reads per tick — minutes per run. This is the biggest performance risk in the design and must be built this way from the start:

- Chebyshev rings `r = 1..SearchRadius(12)`, stop at the first ring containing food → worst case 625 cells, typically a handful.
- Before the fine scan, consult `blockFood` (16×16 grid of 8×8-cell totals): if the agent's block and its eight torus-neighbours are all zero, skip straight to `wanderStep`. Exactly the famine case, where every agent would otherwise pay the full 625-cell scan every tick.
- Ring cell order from a precomputed offset table built once in `init()`.
- **Tiebreak bias:** a fixed intra-ring scan order makes every agent prefer the same compass direction → visible collective drift artefact. Rotate the ring's start index by `AgentRand(seed, id, tick, purposeTiebreak).Intn(ringLen)`. Deterministic, bias gone.

**Torus topology.** `Width = Height = 128 = 2^7`, so `idx = (y<<7)|x` and wrapping is `x = (x+dx) & 127`. Branchless, no boundary special case, no edge-clustering artefact.

---

## Outcome classifier (`internal/stats`) — exact integer arithmetic

**Window interpretation:** **non-overlapping 1200-tick blocks evaluated on each boundary** (10 evaluations per 12 000-tick run, 100 per deep run). This is the only reading under which "3 consecutive windows (30 y)" is consistent, and it removes the ring buffer entirely — five `int64` accumulators updated O(1) per tick, reset at each boundary.

Per window of `n = 1200` samples, accumulate `S1 = Σp`, `S2 = Σp²`, `Sxy = Σ i·p_i`; constants `Sx = n(n-1)/2`, `Sxx = (n-1)n(2n-1)/6`.

| Spec predicate | Exact integer form | Overflow |
|---|---|---|
| `stddev/mean < 0.10` | `100·n·S2 < 101·S1²` | fits int64 (≤6.3e15) |
| `stddev/mean > 0.25` | `16·n·S2 > 17·S1²` | fits int64 |
| `\|slope·n/mean\| < 0.05` | `20·n²·\|N\| < D·S1`, `N = n·Sxy − Sx·S1`, `D = n·Sxx − Sx² = n²(n²−1)/12 > 0` | **needs 128-bit** (≈1.6e20) |
| `slope·n/mean < −0.10` (DECLINING) | `N < 0 && 10·n²·\|N\| > D·S1` | needs 128-bit |

`intcmp.go` provides `mulCmpU128(a, b, c, d uint64) int` comparing `a·b` vs `c·d` via `math/bits.Mul64` — exact, allocation-free, no `math/big`, no floats. Bounds rely on `p ≤ 6553` (OVERRUN is terminal); add a defensive assertion `p ≤ 65535`.

**Classifier state machine** (the six outcomes can overlap, so resolution order is part of the design):

```
per tick:
    if pop == 0                  -> EXTINCT, terminal; extinct_year = tick/120
    if pop > 40% of cells (6553) -> OVERRUN, terminal
    track peak_pop / peak_year
on each 1200-tick boundary:
    windowIndex++                                  # counts CLOSED windows, burn-in included
    if windowIndex <= BurnInWindows -> skip EVERY predicate below; next window
    evaluate cv and slope predicates over the just-closed window
    if stable-predicate && pop >= 20 -> stableStreak++ ; if streak == 3 -> STABLE, terminal
    else                             -> stableStreak = 0
    maxCV     = max(maxCV, cv of this window)
    lastSlope = slope of this window
at MAX_TICK:
    if maxCV > 0.25                       -> OSCILLATING
    elif lastSlope < -0.10 per window     -> DECLINING
    else                                  -> TIMEOUT
```

**Burn-in (`BurnInWindows`, default 1) — ratified after P3, and it changes what OSCILLATING means.**

Window 1 of every run contains the startup transient: the founding population is fed by the initial pantry, booms far past the carrying capacity, and crashes back. That window can never satisfy the stable predicate, and its coefficient of variation latches `maxCV` above 0.25 for the rest of the run. OSCILLATING is the first branch of the end-of-run fallback, so **every run that failed to string together three stable windows was labelled OSCILLATING** — the label meant "did not reach STABLE in time", not "genuinely oscillating", and it would mislabel whole regions of a parameter sweep.

The rules:

- Windows with index < `BurnInWindows` **are still accumulated and still advance the boundary**, so window numbering and the tick timeline are unaffected.
- They are excluded from **every** predicate — the stable streak, `maxCV`, and the declining-slope fallback alike. A burn-in that some predicates respected and others ignored would be worse than none, because the verdict would answer no single question. (Suppressing only `maxCV` while still letting the crash reset the streak, or the reverse, each reintroduce half the original bug.)
- The **per-tick** EXTINCT and OVERRUN checks still fire during burn-in. Burn-in excludes window predicates, not the run's terminal conditions: a run that dies inside its burn-in must not be reported as an uneventful timeout.
- `BurnInWindows = 0` reproduces the pre-burn-in behaviour **exactly**, which is what makes the change provably opt-out; `TestBurnInZeroIsExactlyTheOldBehaviour` pins that with hard-coded expectations rather than derived ones.
- A burn-in longer than the whole run is legal and must not panic: no predicate is ever evaluated and the run resolves as TIMEOUT.

`BurnInWindows` is a **classification** parameter, not a simulation one — it cannot move a single state hash. It nevertheless lives in `sim.Config` and therefore in `config_hash`, for two reasons: two runs classified with different burn-ins are not comparable even when their trajectories are bit-identical, and a sweep addresses its axes by `Config` field name, so a knob that is not a `Config` field cannot be swept. `MaxTick` is in `Config` on the same footing.

Measured effect on the eight golden seeds: seven were STABLE before and after (STABLE is terminal and is reached before the fallback ever runs, so burn-in correctly neither helps nor hurts them), and seed 5 moved OSCILLATING → DECLINING with its `state_hash` unchanged.

**Record** — one row per run, fixed CSV column order, LF endings, hash as `%016x`:

```
seed, outcome, peak_pop, peak_year, extinct_year, final_pop, final_tick, state_hash, config_hash, version, wall_ms
```

Under `--sweep`, one column per swept axis is appended **after** those eleven, so a row is self-describing and a tool that knows the canonical schema keeps working. `config_hash` is always the hash of the config *that row* used, which in a sweep is the cell's, not the base's.

**`wall_ms` is the one non-deterministic column** (it exists for capacity planning). Every consumer must exclude it: a result-diff tool must ignore it, and any byte-comparison of two result files must blank it first. **Two records with different `config_hash` are not comparable at all**, and a diff tool must refuse rather than try.

Results are collected into a slice and **sorted by cell index, then by seed, before writing**. Two runs of the same batch at different worker counts therefore produce files that are **identical apart from `wall_ms`** — they cannot be byte-identical while a wall-clock column is in the schema, and an earlier draft of this plan wrongly claimed they were. `TestRunnerParallelMatchesSerial` compares `--workers 1` against `--workers 8` with `wall_ms` blanked, in both output formats.

---

## CLI surface

Flat `urfave/cli/v2` app, no subcommands, matching `sgo/main.go`:

```
worldbit [flags]

--headless, -H          run the batch harness instead of the GUI            (default false)
--seed, -s     uint64   world seed; in batch mode the first of --runs seeds (default 1)
--runs, -n     int      number of consecutive seeds to run (batch only)     (default 1)
--ticks, -t    int      MAX_TICK; 12000 batch / 120000 deep                 (default 12000)
--out, -o      string   output path; .csv or .json selects the writer       (default "runs.csv")
--workers, -w  int      batch parallelism                                   (default GOMAXPROCS)
--config, -c   string   JSON config file; absent fields keep their defaults (default none)
--set          k=v      single-parameter override, repeatable:
                        --set InitFoodPerCell=3 --set FoodMax=8
--sweep        string   JSON parameter-grid file; runs every cell of the grid  (default none)
                        batch only; the file's seeds/seed_start govern the
                        seed range, so --seed/--runs are ignored under it
--dump-config  string   write the resolved config as JSON and exit
--verify                enable sim invariant assertions (slow)              (default false)
--hash-every   int      print the state hash every N ticks (0 = off)        (default 0)
--scale        int      GUI pixel scale (128*scale window)                  (default 4)
--version, -v
```

`--ticks` is itself a `Config` field (`MaxTick`), so it participates in resolution like any other: default → file → flag. Same for `--verify`. Flags always win over the file.

**No simulation parameter is ever read from the environment.** Grep for `os.Getenv` in review: the only legitimate uses are none.

Batch seeds are `seed, seed+1, … seed+runs-1` — legible, easy to re-run one by one. Mode dispatch: `--headless` → `runner.Run(...)`; otherwise `runGUI(...)` (`//go:build !nogui`). Version var at package main like sgo, bumped on release, written into every record.

**Build-tag rationale:** ebiten pulls in Cocoa/X11/OpenGL. A CI runner or headless server may lack GL/X11 dev headers, and it would be absurd for the batch harness — the primary deliverable — to be unbuildable there. `make build-headless` (`-tags nogui`) produces a pure stdlib+cli binary. Verify `go build -tags nogui ./...` early in P1, before ebiten is even added, so the boundary can't rot.

---

## GUI: read-only by construction, replay by re-simulation

**Read-only boundary.** The GUI never touches `World` fields. `sim.World.RenderInto(f *Frame)` fills:

```go
type Frame struct {
    Tick, Year int32
    Pop        int32
    Biome      []uint8   // len 16384, copied
    Food       []int16   // len 16384, copied
    AgentAt    []uint8   // len 16384, 1 if any agent occupies the cell
}
```

The render path only ever reads `Frame`. Allocated once and refilled → ~48 KB copied per rendered frame (60 fps → 2.9 MB/s, negligible), and it makes "the GUI mutates the sim" structurally impossible rather than merely forbidden. Backed by `TestGUINeverMutates`: hash, full render pass, hash, assert equal.

**Rendering.** One 128×128 `*ebiten.Image`; paint biome/food colour per cell into an RGBA byte slice, overwrite agent-occupied cells with the agent colour, one `WritePixels` per frame, then `DrawImage` with `GeoM.Scale(scale, scale)` and nearest-neighbour filtering. Food as a green ramp over `0..FoodMax`. Sparkline: population history downsampled to widget width, `vector.StrokeLine`. HUD via `ebitenutil.DebugPrintAt` initially (zero font dependency); a proper `text/v2` face is later polish, not a blocker.

**Replay: re-simulate, do not store.** The concrete payoff of the determinism work:
- Snapshots would be ~100 KB × 12 000 ticks ≈ 1.2 GB per seed, plus a serialisation format to version.
- Re-simulating 12 000 ticks costs well under a second and by construction reproduces the bit-identical trajectory the batch run classified.
- **Free end-to-end verification:** when the GUI finishes replaying seed S to tick T, hash the world and compare against the CSV's `state_hash`. Match → dimmed; mismatch → red banner. Every replay becomes a live determinism regression test against a real recorded result, catching drift that unit tests would only catch at the next `make golden`.
- Backward scrubbing is the one weak case. Mitigate with an **in-memory** snapshot ring: clone every 1 000 ticks (12 clones × ~80 KB ≈ 1 MB); scrub jumps to the nearest earlier snapshot and re-steps forward. Pure optimisation, never written to disk, never part of a persisted format.

---

## Phased task order

Determinism harness precedes gameplay throughout. Each phase ends green on `go test ./...`, `go vet ./...`, `go build -tags nogui ./...`.

### P1 — Repo skeleton + determinism harness (no agents yet)
1. Scaffold via gobuild; apply the five post-scaffold fixes.
2. `config.go` — `Config` (with `json` tags), `DefaultConfig()`, `Config.Hash()`, `Config.Validate()`.
2b. Config loading in `main`/`internal/config`: `--config` file, `--set` overrides, `--dump-config`; resolution order default → file → flags; `Validate()` before anything runs. Tests: `TestConfigRoundTrip`, `TestPartialConfigKeepsDefaults`, `TestValidateRejects`, `TestConfigHashChangesWithEveryField`.
3. `world.go` — `Cell`, `Agent`, `World`, `WorldView`, `NewWorld(seed, cfg)` creating cells only (`InitFoodPerCell`, all `BiomePlain`), zero agents.
4. `rng.go` — splitmix64, `AgentRand`, `Rand.Next`, `Rand.Intn`.
5. `env.go` — torus index helpers, stride regrowth.
6. `tick.go` — `Step` with phase 1 and the tick increment only.
7. `hash.go` — inlined FNV-1a canonical hash.
8. **`determinism_lint_test.go`** — the AST guard. Write this before anything else in `sim` grows.
9. **`TestSameSeedSameHash`** over 12 000 ticks, checkpoint every 100.
10. **`TestGoldenHashes`** + `-update` machinery + committed `testdata/golden_hashes.csv`.
11. `TestSubstreamIndependence` — agent 7's draw sequence identical whether or not agent 5 exists.
12. `TestRegrowthStrideCoversEachCellExactlyOnce` over 200 ticks; `TestFoodNeverExceedsMaxOrGoesNegative`.
13. `main.go` with the full flag set, both modes stubbed; `gui_stub.go` + `gui_enabled.go` (empty stub for now) — prove `-tags nogui` builds.

*Exit: a world with no agents is hash-stable across runs and pinned by golden fixtures, and the lint test rejects a deliberately introduced `map[int]int` in `sim`.*

### P2 — Agents
1. `intent.go`, `agent.go` (`decide`, `stepToward`, `wanderStep`), ring offset table.
2. `index.go` — `blockFood`, incremental maintenance on eat/regrow, `rebuildBlockIndex` for verification.
3. `findNearestFood` with block-skip and rotated ring tiebreak.
4. `NewWorld` seeds `InitAgents` at distinct pseudo-random cells, ages spread over `[0, InitAgeSpread)`.
5. Extend `Step` with phases 2–6.
6. `invariants.go` + `Verify`: agents strictly ascending by ID; energy ∈ [1, EnergyMax]; food ∈ [0, FoodMax]; block index equals a fresh rebuild; **energy conservation** (Δ total energy + 10·Δ total food equals the sum of known sources and sinks: regrowth in; burn, birth overhead, death loss, over-cap eating waste out). The conservation check is the single strongest bug net in the project — a subtly wrong eat or birth shows up here immediately.
7. Regenerate goldens; add `TestDeepRunNoOverflow` (120 000 ticks) and `BenchmarkRun12000`.

*Exit: `--headless --seed 1 --ticks 12000 --verify` completes, invariants hold every tick, benchmark at or under ~1 s per run. If not, tune `SearchRadius` / block granularity **here**, not later.*

### P3 — Stats and classifier
1. `intcmp.go` + tests against a `math/big` reference on random inputs (reference lives in the test, never in production code).
2. `window.go` accumulators; `classify.go` state machine; `recorder.go`.
3. `TestClassifyKnownSeries` — hand-built series for each of the six outcomes.
4. `TestClassifierPredicatesMatchRationalReference` — integer predicates vs `big.Rat`, exact equality.

### P4 — Batch runner and parameter sweeps (core deliverable complete at the end of this phase)
1. `record.go`, `runner.go` (worker pool, one `World` per worker, no pooling, no shared state), `output.go` (CSV + JSON, sorted by cell then seed, `config_hash` + `version` in every row and in the JSON header).
2. `TestRunnerParallelMatchesSerial` — `--workers 1` and `--workers 8` produce output files identical apart from `wall_ms`.
3. `TestOutputSortedBySeed`.
4. Wire `--headless`, `--runs`, `--ticks`, `--out`, `--workers`.
5. Smoke: `--headless --runs 1000 --out runs.csv`, eyeball the outcome distribution. **If everything is EXTINCT or everything is OVERRUN, stop and revisit `InitFoodPerCell` / `ReproEnergyCost` with the user rather than tuning silently** — the parameter set is theirs.
6. Classifier burn-in (`BurnInWindows`, default 1) — see the classifier section. Do this **first**: it changes what the phase's own output means.
7. Parameter sweeps (`--sweep`), below.

**Early stop on a terminal outcome.** EXTINCT and OVERRUN end the run where they happen and the actual tick goes into `final_tick`. An overrun run costs roughly 13× a normal one per tick, which across a sweep is the difference between minutes and hours; an extinct run finishes in a few milliseconds instead of half a second.

STABLE is terminal for the *classifier* but deliberately does **not** stop the simulation. `final_pop`, `final_tick` and `state_hash` describe the world where the simulation stopped, and cutting stable runs short at their third stable boundary would make those three columns describe a different point in time for stable runs than for every other kind — and would break replay verification, which re-simulates to `final_tick` and compares hashes. `TestEarlyStopDoesNotChangeTheOutcome` pins that the optimisation is invisible in the verdict, and fails loudly if the configurations it uses stop reaching a terminal outcome (an early-stop test that never exercises the early stop proves nothing).

#### Parameter sweeps

`--sweep <file.json>` describes a grid:

```json
{
  "seeds": 20,
  "seed_start": 1,
  "axes": {
    "InitFoodPerCell": [1, 3, 5],
    "ReproEnergyCost": [30, 40, 50]
  }
}
```

- **Axis keys are `Config` field names — the same PascalCase identifiers `--set` uses**, resolved through the *same* exported `config.SetField`. There must not be a second name-matching path: two spellings that drift apart would break the promise that a sweep row is reproducible from its own command line. `TestSweepUsesTheSetFieldResolution` compares a cell's config against the equivalent `--set` and requires identical hashes.
- **Unknown axis keys are a hard error**, consistent with `--config` and `--set`, and so are axis values that cannot be applied to the field they name (`FoodMax: [99999]`). Both are reported once at load, before anything runs, because they would fail identically in every cell. A silently ignored axis would produce a grid of *identical* cells that looks entirely plausible.
- Axis order is **sorted by key name**, not the order the JSON object listed them in: the axes arrive in a map, Go map iteration is randomised, and cell numbering, column order and the aggregate all depend on that order being fixed.
- Cells are the Cartesian product, last axis varying fastest. Each runs `seeds` runs (`seed_start … seed_start+seeds-1`); total runs = cells × seeds.
- `seed_start` decodes through a pointer so an absent key defaults to 1 while an explicit `0` stays `0` — seed 0 is a legal seed.
- The base config resolves as usual (default → `--config` → `--set`); each cell layers its axis fields on top, then **`Validate()` runs per cell**. An invalid cell is reported by name and **skipped, not fatal** — a grid deliberately spans territory the parameters cannot all reach, and losing the whole sweep to one impossible corner (say `ReproEnergyCost` above `ReproEnergyMin`) would make wide grids unusable. The count of skipped cells is logged. A sweep in which *every* cell is invalid is an error, not an empty results file. Surviving cells keep their index in the full product, so a cell's identity does not shift when a neighbour is dropped.
- **The total run count and a wall-time estimate are printed before the first tick.** This is the "no silent caps" rule: there is no cap, so knowing what you launched is the only protection against accidentally launching a six-hour sweep.
- `config_hash` per row is that row's own resolved config, never the base.

Two outputs, both named by appending to `--out` (appending rather than substituting, so two outputs differing only by extension cannot collide on one sidecar):

| File | Contents |
|---|---|
| `<out>` | the per-run CSV/JSON as usual, plus one column per swept axis |
| `<out>.config.json` | the resolved **base** config, written for every batch, sweep or not |
| `<out>.cells.csv` | one row per cell: axis values, the count of each of the six outcomes, mean/median peak and final population, and the cell's `config_hash` |

The aggregate is what actually answers "which parameter region is interesting"; the per-run file says what each individual seed did. Means are summed in integers and divided exactly once, so the floating-point result cannot depend on the order runs completed in, and are formatted to one decimal place so the file stays byte-stable.

**Placement:** sweep parsing and expansion live in `internal/config`, not `internal/runner`, so that the one-way dependency `runner -> {sim, stats}` holds. `main` maps `config.SweepCell` onto `runner.Cell`.

**Tests:** sweep output identical across worker counts (per-run file *and* aggregate); a 2×2 grid produces exactly cells×seeds rows; an unknown axis key errors and writes no output file; an invalid cell is skipped while the rest still run; every-cell-invalid is an error; cell aggregation counts checked against hand-built records.

### P5 — GUI
1. `frame.go` + `RenderInto` in `sim`; `TestGUINeverMutates`.
2. `game.go`, `render.go`, `sparkline.go`, `hud.go` (tick, year, pop, outcome-so-far, state hash).
3. `input.go` — pause / 1× / 10× / 100× / max, restart, seed entry.
4. `snapshot.go` — in-memory snapshot ring for backward scrubbing.
5. Replay verification: `--seed S` plus optional `--expect-hash`, red mismatch banner.

### P6 — Polish and docs
1. Biome generation (integer value noise from the seed; water impassable, never regrows) behind a config knob, default off. Water reduces effective carrying capacity roughly in proportion to the land fraction — note in README.
2. Repo `CLAUDE.md` and `README.md`.
3. Add the repo-list bullet to the platform-root `CLAUDE.md`.
4. Build the knowledge graph, then `embed_graph_tool`.
5. Security scan (platform SessionStart hook expects every repo scanned within 7 days).

---

## Experimental findings (P4, 2026-07-31)

The first real results the harness has produced. Recorded here because they are what P5 and P6 will be reasoned from, and because two of them are corrections to assumptions in this plan.

Method: a 45-cell sweep over `InitFoodPerCell` [1,3,5] × `ReproEnergyCost` [30,40,50] × `FoodRegrowTicks` [25,100,400,1600,6400], 20 seeds per cell (900 runs, 37 s), plus a 12-cell `BurnPerTick` × `FoodRegrowTicks` grid and a 6-cell probe of the near-overrun band.

**1. `FoodRegrowTicks` dominates; it is the carrying-capacity knob.** Capacity is `cells / FoodRegrowTicks × EnergyPerFood / BurnPerTick`, and the outcome follows it almost deterministically:

| `FoodRegrowTicks` | capacity | outcome over 180 runs |
|---|---|---|
| 25 | ~6550 | OVERRUN, every seed |
| 100 | ~1640 | STABLE, every seed |
| 400 | ~410 | STABLE/TIMEOUT mix, a few DECLINING |
| 1600 | ~102 | TIMEOUT, a few DECLINING |
| 6400 | ~26 | OSCILLATING, every seed |

**2. `InitFoodPerCell` and `ReproEnergyCost` only move the opening boom height, not the outcome.** Across the `InitFoodPerCell` axis at a fixed `FoodRegrowTicks`, mean peak population moves by 4–5× (677 → 3074 at regrow 6400) while the outcome distribution does not move at all. This *revises risk item 1*, which called `InitFoodPerCell` "the highest-leverage unknown" on the reasoning that the first boom decides whether runs classify OVERRUN. The boom does scale as predicted; it simply does not decide the classification, because OVERRUN at the default grid is reached only where the *sustained* capacity is near the threshold. `FoodRegrowTicks` — which the plan never listed as an open question — is the parameter that deserved that billing.

**3. EXTINCT needs a metabolic squeeze, not starvation.** Slowing regrowth does not extinguish a population: even `FoodRegrowTicks=12000` (capacity ~14) leaves a surviving remnant in every seed. Raising `BurnPerTick` does: 5 extinguishes every seed at regrow ≥ 1600, and 2–3 gives a 55–90 % extinction rate. The reason is that the search-and-eat loop keeps a small population alive on a trickle indefinitely, so the way to kill it is to raise the per-agent floor cost rather than lower the supply.

**4. Large-amplitude sustained oscillation appears not to exist anywhere in the space probed — and this is a finding about the CLASSIFIER, not only the ecology.** Every OSCILLATING cell sits at mean final population 9–20 against peaks of 600–3000. That is a starving remnant of 10–40 agents whose small-number noise trivially clears the coefficient-of-variation line, because CV is **scale-free**: a population wandering between 10 and 40 has a far higher CV than one wandering between 700 and 900, though only the latter is what "oscillating ecology" is meant to describe. Two checks confirm the reading:

- Walking `BurnInWindows` from 0 to 9 on such a cell shows high variation persisting through ~7 of 10 windows and then decaying into DECLINING/TIMEOUT — a long slow collapse, not a limit cycle.
- The near-overrun band where genuine overshoot cycles would live (`FoodRegrowTicks` 30/40/50/60/75/90, defaults otherwise, 20 seeds each) produced **not one** OSCILLATING run: 20/20 OVERRUN at 30, an 11/9 STABLE/OVERRUN split at 40, and STABLE from 50 up (119 STABLE and a single TIMEOUT at 75 across the four slowest cells). No oscillation at large population anywhere.

**A future reader must not trust the OSCILLATING label as evidence of a cycling ecology.** If that distinction matters later, the fix is a floor on the CV predicate (an absolute amplitude alongside the relative one) or a minimum population for the high-variation flag, mirroring the `MinStablePopulation` floor the stable predicate already has — not a parameter change. This is a design question for the user, not a bug.

**5. The ratified defaults sit in a deliberately quiet region.** At `FoodRegrowTicks=200`, between the all-STABLE and mixed bands, a 1000-seed batch is 966 STABLE / 33 TIMEOUT / 1 DECLINING, with EXTINCT, OVERRUN and OSCILLATING unreachable and the peak at year 6 in all eight golden seeds. Moving `FoodRegrowTicks` one notch changes the distribution more than 1000 seeds do. **The defaults were not retuned** — they are the user's, and this map is the evidence for that decision rather than a licence to make it. Note for anyone exercising the classifier: use a sweep, not a seed batch. `FoodRegrowTicks` 300–500 is where STABLE, TIMEOUT and DECLINING coexist inside one cell.

---

## Migrations

**None.** No database of any kind. The only persisted artefacts are CSV/JSON outputs — disposable experiment results, not state. `testdata/golden_hashes.csv` is the one committed data file; its "migration" procedure is `make golden` plus a commit message explaining which behavioural change justified it.

---

## Testing strategy

`go test ./...` is the whole gate — no service to boot, no DB to seed.

- **Determinism (P1, first):** `TestDeterminismLint`, `TestSameSeedSameHash`, `TestGoldenHashes`, `TestSubstreamIndependence`.
- **Sim invariants (P2):** ascending IDs, energy/food/age bounds, block index equals rebuild, energy conservation per tick, regrowth stride coverage, `TestDeepRunNoOverflow` at 120 000 ticks.
- **Numerics (P3):** integer predicates vs `big.Rat`; classifier vs hand-built series for all six outcomes; `Rand.Intn` bucket uniformity.
- **Runner (P4):** parallel output identical to serial apart from `wall_ms` (in both CSV and JSON); sorted by cell then seed; early stop does not change the verdict; `config_hash` present, per-row, and stable.
- **Sweeps (P4):** grid expansion and axis ordering; unknown axis key and unusable axis value rejected at load; invalid cell skipped while the rest run; every-cell-invalid is an error; cell aggregation checked against hand-built records; sweep output identical across worker counts for both the per-run file and the aggregate.
- **Burn-in (P4):** a wild opening followed by a flat tail classifies STABLE; `BurnInWindows=0` reproduces the old behaviour with hard-coded expectations; a burn-in longer than the run yields TIMEOUT without panicking; each of the three predicates is separately shown to respect it; EXTINCT and OVERRUN still fire inside burn-in.
- **GUI (P5):** `TestGUINeverMutates`. Rest verified manually.
- **Performance guards:** `BenchmarkStep`, `BenchmarkRun12000`, documented target ≈1 s per 12 000-tick run. Watch in review, not a hard failure.

**Measured at P4** (default parameters, 12 000 ticks, arm64):

| Condition | Cost per run |
|---|---|
| One run on its own (`--workers 1`) | **~350 ms** |
| Effective, under a pool saturating 8 cores | **~553 ms** |

The ≈1 s target is met with room to spare, but the **parallel scaling caveat is new information**: 1 000 runs take 69 s wall for 429 s of CPU on 8 workers, i.e. roughly 1.6× the solo cost per run rather than the 1.0× a linear-scaling job would show. The simulation is memory-bound over the 16 384-cell grid, so workers contend for bandwidth. Plan sweep budgets on the saturated figure, not the solo one — which is what `runner.EstimatedDuration` does, deliberately over-estimating a `--workers 1` batch because that is the harmless direction to be wrong in. Early stop then makes the estimate conservative in practice: a 900-run sweep estimated at 62 s finished in 37 s because its EXTINCT and OVERRUN cells stopped early.

**Manual:**
1. `make headless` → 100 seeds, inspect the outcome mix.
2. Pick an interesting seed, `go run . --seed <S>`, confirm the GUI's final hash matches the CSV's `state_hash`.
3. `make build-headless` in a bare container to confirm the tag boundary holds.

**Cross-architecture:** integer-only, map-free, single-threaded code should be bit-identical across arm64 and amd64. Worth one confirmation: build `GOARCH=amd64` under Rosetta, compare `--hash-every 1000` for seed 1 against arm64. Cheap, and the only remaining place the contract could be silently false.

---

## Risks / open questions

**Unspecified parameters that materially change the outcome distribution** — defaults proposed so the coder isn't blocked, but each deserves a decision:

1. **`InitFoodPerCell` — highest-leverage unknown.** *(Superseded by measurement at P4 — see "Experimental findings", item 2: the boom scales as predicted but does not decide the classification, and `FoodRegrowTicks` turned out to be the parameter that deserved this billing. Kept as written for the record.)* The first boom is fed almost entirely by the starting pantry, so this, not the regrowth rate, decides whether early runs classify OVERRUN. At 5 (full board), standing 81 920 food ≈ 819 200 energy supports an ~8 000-agent overshoot — above the OVERRUN threshold of 6 553, meaning OVERRUN would largely measure the initial condition. At 1 the ceiling is ~1 600. **Proposed: 1.**
2. **`FoodMax` — not specified at all.** Uncapped, a 120 000-tick deep run accumulates up to 600 food (6 000 energy) in one cell, turning cells into infinite larders and making famine impossible. **Proposed: 5.** Accepted consequence: once the board saturates, regrowth on full cells is wasted, so effective production falls below 82 food/tick near full recovery — sensible, but it bounds recovery speed.
3. **Reproduction energetics.** **Proposed:** `ReproEnergyMin 60`, `ReproEnergyCost 40`, `ChildEnergy 30` (10 lost as overhead, a mild damper on runaway growth). Conservative variant: cost 30 / child 30, no loss.
4. **Hunger threshold. Proposed: 70.** Implies eating is skipped when it would waste more than one food-unit of energy; the cap makes some waste unavoidable and intended.
5. **Initial agents: count, energy, and critically age spread. Proposed: 50 agents, energy 50, ages uniform over [0, 240).** If all founders start at age 0 they mature and die on the same tick, producing a synchronised cohort that makes almost every run classify OSCILLATING for reasons that are an initialisation artefact, not ecology. Age spreading is not cosmetic.
6. **Grid topology. Proposed: torus** (`& 127`). Branchless, no boundary code, no corner pooling. Hard walls change the dynamics noticeably.
7. **Movement. Proposed:** 1 cell/tick, 8-neighbourhood, step reducing Chebyshev distance to target, ties by fixed priority order.
8. **Biome semantics.** Field exists from P1 but uniformly `BiomePlain`; water arrives in P6 behind a knob.

**Design decisions needing ratification:**

9. **Nearest-food search bounded at radius 12, not global.** Literal "nearest food on the board" is ~13 M cell reads/tick — misses the ~1 s/run target by two or three orders of magnitude. Behavioural change: an agent with no food within 12 cells wanders rather than beelining across the map. If unbounded nearest is semantically required, a multi-level food quadtree gives it — more code, same determinism properties.
10. **OVERRUN terminal**, like EXTINCT. The brief only marks EXTINCT stop-immediately, but a run reaching 6 553 agents costs ~13× a normal run and dominates batch wall-time while telling you nothing new.
11a. **Burn-in on the classifier's windows** (`BurnInWindows`, default 1), ratified after P3 and built at the start of P4. Without it OSCILLATING means "did not reach STABLE in time"; see the classifier section for the rule and "Experimental findings" item 4 for what OSCILLATING still does *not* mean.

11. **Non-overlapping 1200-tick windows** rather than a per-tick sliding window. Only reading consistent with "3 consecutive windows (30 y)"; collapses the classifier to five O(1) accumulators, no ring buffer. A true sliding window with a 120-tick stride is a small change but needs int128 comparisons on every evaluation and a restated "3 consecutive" rule.
12. **DECLINING threshold. Proposed:** slope·1200/mean < −0.10 (losing >10 % of window mean per window).
13. **Phase-separated single buffer instead of a literal state copy.** Same guarantee, zero copy; a literal double buffer costs ~768 MB of memcpy per run for no added safety.
14. **`internal/` rather than sgo-style `pkg/`.** Trivially reversible.

**Ordinary risks:**

15. **ebiten and CGO.** Needs Cocoa/X11/GL at link time on most platforms. Mitigated by the `nogui` tag from P1. Also: `ebiten.RunGame` must run on the main goroutine — do not wrap it.
16. **`sort.Slice` is unstable.** Anywhere in `sim` or `runner` that sorts, use `sort.SliceStable` with a total order (agent ID, or seed). Add to the conventions list.
17. **Golden-hash regeneration is the failure mode most likely to hollow out this project.** `make golden` makes a failing test pass, so it's the path of least resistance both when a legitimate refactor breaks the hash and when a real determinism bug does. The repo CLAUDE.md must state that regenerating goldens requires a commit message naming the behavioural change that justified it, and that a golden change accompanying a "pure refactor" is a red flag to investigate, never to accept.
18. **The knowledge graph is unavailable for this plan** (repo doesn't exist); blast-radius analysis is from first principles. Build after P2.

---

## Repo `CLAUDE.md` contents

Tone modelled on `speedtest/CLAUDE.md` and `sgo/CLAUDE.md`, determinism contract promoted to the top:

1. **Purpose** — deterministic agent-based world simulator; the real workflow is batch-classify-then-replay; an experiment harness with a viewer, not a game.
2. **Standalone, not a platform service** — reuse the sgo/speedtest wording: no domains, no DI, no clean-arch layers, no mprocs/hosts entries; the kuery shared-pkg rule and the service template do not apply; no local `pkg/` to consolidate.
3. **The determinism contract** (longest section, first): the guarantee; banned constructs in `internal/sim` (maps of any kind, floats, `time`, `math/rand`, `crypto/rand`, goroutines, channels, I/O); that `determinism_lint_test.go` enforces them via AST and its import allowlist is where a deliberate exception is made; the per-agent RNG substream rule and why a shared stream is forbidden; the fixed-tick rule and the one legitimate appearance of wall-clock in the GUI's "max" speed.
4. **Package boundaries** — the one-way graph; `sim` imports stdlib only; the GUI sees `Frame`, never `World`.
5. **Tick phase order** — the six phases, plus: changing the order, the tiebreak scheme, or the RNG mixing changes every hash and requires golden regeneration with justification.
6. **Golden hashes** — how to regenerate, and that regenerating is a deliberate act with a commit-message justification, never a reflex when a test goes red.
7. **Parameters** — `internal/sim/config.go` is the single source of truth; never hardcode a constant inline (mirrors sgo's rule about `pkg/analyzer/config.go`). Include the carrying-capacity arithmetic (16 384 ÷ 200 × 10 ÷ 1 ≈ 820) so a future reader can sanity-check a parameter change, plus "population above ~5 000 means a bug".
7b. **External config** — the resolution order (default → `--config` file → `--set`); that `config_hash` is computed on the resolved config; that `--dump-config` is how you record an experiment; and the hard rule that **no simulation parameter may ever come from an environment variable**, with the reason (ambient, machine-local, invisibly divergent). Adding a field to `Config` means adding it to `Hash()`, `Validate()`, and the JSON tags — `TestConfigHashChangesWithEveryField` enforces the first. Also: parameter names resolve through exactly one function, `config.SetField`, shared by `--set` and sweep axis keys — never add a second name-matching path.
7c. **Classifier burn-in** — `BurnInWindows` (default 1) excludes leading windows from every window predicate but not from the per-tick terminal checks; 0 reproduces the pre-burn-in behaviour; it is a classification parameter that lives in `Config` so it travels in `config_hash` and can be swept. And the caveat from "Experimental findings" item 4: **OSCILLATING currently means high relative variation, which a starving remnant of a dozen agents satisfies trivially** — do not read it as a cycling ecology.
8. **CLI surface and commands** — flag table (including `--sweep`), `make` targets, the `nogui` build tag.
9. **Output schema** — CSV columns; that `wall_ms` is the one non-deterministic column and every consumer must exclude it (two runs at different worker counts differ in that column and nothing else); the sweep's appended axis columns and the two extra files (`<out>.config.json`, `<out>.cells.csv`); the meaning of `config_hash`, that it is per-row and per-cell, and that records with differing `config_hash` are not comparable.
10. **Conventions** — no maps; integer only; `sort.SliceStable` by ID; hash canonical state only, never derived caches (and why); `.code-review-graph/` gitignored; `.env` carries only `PRJ`/`VERSION`, never simulation parameters.

## Line for the platform-root `CLAUDE.md` repo list

Insert after the `speedtest/` bullet:

> - **`worldbit/`** — Deterministic agent-based world simulator ("WorldBox-lite", module `github.com/vukyn/worldbit`, scaffolded from the gobuild `base` preset). **Idle/observation sim as an experiment harness**: run N seeds headless → classify each outcome (extinct/stable/oscillating/overrun/declining/timeout) into CSV/JSON → replay an interesting seed in an ebiten GUI. Three strictly one-way packages: `internal/sim` (pure integer-only logic, stdlib-only, single-threaded, **no maps, no floats, no `time`, no `math/rand`**), `internal/runner` (worker pool, parallel across runs only), `internal/gui` (ebiten, read-only over a `Frame` snapshot; `-tags nogui` builds a headless-only binary for CI/servers). **Bit-exact determinism is the hard constraint** — same seed + config + binary → identical FNV-1a state hash at every tick — enforced by an AST lint test and golden-hash fixtures in `testdata/` (regenerating them requires a justified commit message). GUI replay re-simulates from the seed rather than loading snapshots. Standalone binary like sgo/gobuild/speedtest — **no DB/DI/domains/clean-arch/kuery/SSO/mprocs/hosts entries**, so the service template and the kuery shared-pkg rule do not apply.
