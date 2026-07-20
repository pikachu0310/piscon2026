# Score and capacity frontiers

## Score champion

- Status: B5 validated
- Commit: `f254b4c`
- Topology declaration: s1=Nginx, s2=MariaDB, s3=Go App
- Benchmark ID / score / validity: `1d79911c-53b2-4613-8316-88e2b990697b`,
  **134,561**, PASSED, deduction 0
- Artifact: `20260720T102008.890588Z-s1-4fc66b`
- Deployed app source provenance: `f254b4c`
- B5 App binary SHA-256: `0c24a8b285540e7be86e95f1cc86728f743e98f8ab8b74513dff849dcbf2f72f`
- B5 Nginx main SHA-256: `67112cca9ad9a60a196df50a6819e7c1a9ca53ef99eb731f502a70a0056071f5`
- B5 Nginx site SHA-256: `9ad5ce44a6e0417c104b8db6605a08c64a2a9ec6debd59a0b30a818b432e81af`
- B5 topology SHA-256: `d399071c8ed066d29ce9e942d3a604cd823b6c1151bb885c89c76fe4987fbc24`
- s2 MariaDB tuning SHA-256: `b7462f1f41615fa7a733871d2aed3969973708e02497d0f5526e8e4e10ea31af`
- B0 remains fully restorable from the earlier manifest and the server-side
  `/home/isucon/score-champion-b0` config backup.

## Capacity frontier

### B1: ingress buffering (`891c84e`)

- Score: 131,466, PASSED, deduction 0 (-2.1% from B0)
- Valid work: condition 202 fell 6.4%; registration success fell 1.9%
- Unit cost: App CPU samples fell 14.7%, about 9% per successful condition
- Bottleneck shift: Go handler body wait disappeared and read tails improved,
  but Nginx-side slow uploads produced 40.5% more client-aborted 499 responses
- Decision: mechanism retained as a branch commit, but not used alone. Restore
  B0 streaming and revisit only with a downstream admission/worker design.

### B2: runtime static compression (`367dd65`)

- Score: 127,939, PASSED, deduction 0
- Demand signal: offered condition attempts increased to 263,690
- Resource shift: vendor body fell 72%, while s1 Nginx CPU became the new
  bottleneck and condition 499 rose to 31,870
- Decision: preserve compression as a capacity mechanism, replace runtime
  compression with prebuilt `.gz` files in B3

### B3/B4: precompressed static assets (`da4fd9a`)

- Scores: 125,165 and 128,296, both PASSED, deduction 0
- Valid work on repeat: 242,988 condition 202, only 0.3% below B0
- Unit cost: App CPU samples 34.31 seconds, 7.3% below B0; condition 499
  reduced 25.3%; s1 transmission fell about 31%
- Remaining downstream: registration success was 868 versus B0's 893
- Decision: confirmed capacity frontier. Keep while condition memory/GC and
  registration downstream experiments attempt to convert it to score.

### B5: compact condition state (`f254b4c`)

- Score: 134,561, PASSED, deduction 0 (new champion)
- In-use heap: -61.7%; history retained bytes: -70.4%
- App CPU samples: -24.7%; GC scan CPU: -61.0%
- Condition 499: 3,073 -> 71
- Decision: structural capacity frontier promoted to score champion. Continue
  feeding the freed capacity via client-arrival and registration experiments.

## Decision rule

The score champion is always restorable and is the final-mode candidate. A
capacity frontier is kept on its own commit/config while up to three downstream
experiments (five for a structural change with at least 20% more valid work)
attempt to convert capacity into official score.
