# Score and capacity frontiers

These are intentionally separate. The scalar score answers “what did this
benchmark reward?”, while the capacity frontier answers “how much valid work
did the system complete, with what failures and resource cost?”. A change can
improve the latter and temporarily lower the former by admitting enough extra
work to move saturation downstream.

## Score champion

- Status: B9 validated
- Commit: `9d025e4`
- Topology declaration: s1=Nginx; s2=MariaDB plus registration/condition-edge
  App; s3=authoritative compact-state/read App
- Benchmark ID / score / validity: `f8ee8044-c525-4eaa-9a3d-a9af2cf51953`,
  **160,102**, PASSED, deduction 6
- Artifact: `20260720T110736.312178Z-s1-18f414`
- Deployed app source provenance: `9d025e4`
- B9 App binary SHA-256: `359b5e431fcbd8dccd731961683e626401affe4d8b4f70e6ad4267d15435fc1c`
- B6 Nginx main SHA-256: `10cb2bce3bb674077c960f0a3910972d7184b97cec111669e40dab64514328b9`
- B9 Nginx site SHA-256: `9f43aefdb8ad8fa74bfac64a49ec0c3576daa5b47bcc48596b2a5ca2c7c122fe`
- B9 s2 drop-in SHA-256: `6e679970dfebd88caa4c69010016e8a7479baebe18913e1927d255d6fd3e0c23`
- B9 s3 drop-in SHA-256: `5b779468eb28ee1738127950bf3609a39cf59f5456c8b15afacc6ebe6843b61c`
- s2 MariaDB tuning SHA-256: `b7462f1f41615fa7a733871d2aed3969973708e02497d0f5526e8e4e10ea31af`
- B9 is backed up server-side under `/home/isucon/score-champion-b9`. B6 remains
  fully restorable under `/home/isucon/score-champion-b6`; B0 remains under
  `/home/isucon/score-champion-b0`.

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

### B6: compact state with restored client demand (`6ee2209`)

- Score: 142,430, PASSED, deduction 0 (new champion)
- Registration success: 896; condition 202: 246,225
- Condition 499: 544, still 82.3% below B0
- Decision: compact state is now converted into official score. This exact
  App/config combination is the rollback target for topology experiments.

### B7/B8: registration-only App on s2 (`bb0d619`)

- Scores: 134,195 and 136,587; B7 PASSED, B8 portal status refresh pending
- B8 accepted work: registration 201 rose 896 -> 901, while condition 202 fell
  246,225 -> 244,519 and condition 499 fell 544 -> 99
- Resource shift: s2 App used 3.25 CPU-seconds but accumulated 198.24 seconds
  of JIA HTTP wait; s3 average busy CPU fell 43.1% -> 40.5%
- Correctness machinery: exact method routing, shared-DB positive UUID
  read-through, no negative cache, registration-only cache suppression, and an
  initialize gate that drains remote registrations before DB reset
- Decision: retain as an isolation/capacity branch. It has not yet proven more
  total accepted work or a higher score. Try to convert the spare-node topology
  by offloading condition ingress/decode while keeping B6 fully restorable.

### B9: compact condition edge on s2 (`9d025e4`)

- Score: 160,102, PASSED, deduction 6 (new score champion)
- Read capacity: trend 200 +21.3%; condition-read 200 +8.0% versus B6
- Write capacity: condition 202 -59.3%; condition 499 544 -> 117,143;
  registration 201 -22.8%
- Resource shift: s3 CPU samples fell 32.26 -> 26.08 seconds, while s2 App
  reached 53.31 seconds and s2 host became 67.7% busy
- Decision: score champion and proof that read-path isolation matters, but not
  a total-work frontier. Preserve B6 separately as the condition-ingest
  frontier and reduce edge synchronization/profiling cost in follow-ups.

### B10: registration returned to s3 (`e8d6467`)

- Score: 147,446, PASSED, deduction 0
- Accepted work: registration 201 692 -> 999; condition 202 100,209 -> 110,927
- Correctness: registration 499 779 -> 0 and registration 500/502 7 -> 0
- Tradeoff: successful trend and condition reads fell, so score fell 7.9%
  despite the healthier admission and larger accepted write workload
- Decision: current total-work/correctness frontier. Keep B9 as score champion;
  use B10 as the base for compact-hop batching and load balancing.

### B11: batched compact condition hop (`3f0fc62`)

- Score: 156,224, PASSED, deduction 0 (2.4% below B9)
- Accepted work: 241,858 condition 202 and 936 registration 201; condition 202
  is 2.41x B9 and 2.18x B10
- Failures: condition 499 fell 118,419 -> 7,334 from B10; registration had four
  499s and no 500/502
- Read work: 26,025 trend 200 and 23,440 condition-read 200, preserving most of
  B9's score-producing read capacity
- Unit cost: edge App CPU 52.98 -> 41.16 seconds; authoritative App CPU 30.38
  -> 21.85 seconds; total sampled App CPU -24.4% while accepted condition work
  more than doubled. Edge CPU per condition 202 is about 0.170 ms, down 64.4%
  from B10.
- Decision: **current overall capacity/correctness champion** and the base for
  further experiments. B9 remains separately restorable as scalar champion.

## Decision rule

Every run is judged on five axes:

1. official score and validity/deduction;
2. successful work by request family, not only the grand total;
3. overload/correctness failures, especially 499 versus 5xx and deductions;
4. resource cost per accepted unit and the node where saturation moved; and
5. read/write latency and whether newly admitted work created downstream load.

The score champion is always restorable and remains a final-mode candidate. A
capacity frontier is kept on its own commit/config while up to three downstream
experiments (five for a structural change with at least 20% more valid work)
attempt to convert capacity into official score. A lower score alone never
causes rollback when accepted work, correctness, or unit cost materially
improves.
