# Score and capacity frontiers

These are intentionally separate. The scalar score answers “what did this
benchmark reward?”, while the capacity frontier answers “how much valid work
did the system complete, with what failures and resource cost?”. A change can
improve the latter and temporarily lower the former by admitting enough extra
work to move saturation downstream.

## Score champion

- Status: B13 validated
- Commit/config provenance: batched App `3f0fc62`; weighted Nginx `eeafca4`
- Topology declaration: s1=Nginx; s2=MariaDB plus compact condition-edge App;
  s3=authoritative compact-state/read/registration App; condition ingress 3:1
  through s2 versus directly to s3
- Benchmark ID / score / validity: `04b6a6bd-0294-474c-9424-37a5c0603e9a`,
  **162,980**, PASSED, deduction 0
- Artifact: `20260720T114250.931832Z-s1-c1eddf`
- B13 App binary SHA-256: `dbecb0596c0ade48829456738a12b0a70046f6b178fc5974d677df5f575dd841`
- B13 Nginx main SHA-256: `10cb2bce3bb674077c960f0a3910972d7184b97cec111669e40dab64514328b9`
- B13 Nginx site SHA-256: `fc45728f3baeeb614f31c04c62bff367e09919ff2ce7f86a093afa34b5d62602`
- s2 MariaDB tuning SHA-256: `b7462f1f41615fa7a733871d2aed3969973708e02497d0f5526e8e4e10ea31af`
- B13 is backed up server-side under `/home/isucon/score-champion-b13`. B11's
  capacity champion, B9's prior scalar champion, B6's single-App champion and
  B0 remain separately restorable in their corresponding backup directories.

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

### B12: exact batched-edge repeat (`eeafca4` intent, B11 effective)

- Score: 151,449, PASSED, deduction 0
- Accepted work: 245,775 condition 202 (the highest single-run condition count
  so far), 944 registration 201, 25,582 trend 200 and 24,731 condition-read 200
- Deployment evidence: all 261,883 condition attempts still used s2; the
  intended weighted upstream existed only in `sites-available`, while the
  active `sites-enabled` path was an independent regular file
- Decision: retained as a B11 repeat and condition-ingest frontier, not labeled
  as evidence for or against load balancing. B13 is the corrected experiment.

### B13: 3:1 edge/direct condition ingress (`eeafca4`)

- Score: 162,980, PASSED, deduction 0 (new score champion)
- Accepted work: 279,985 condition 202 (new ingest champion), 900 registration
  201, 23,999 trend 200 and 26,468 condition-read 200
- Actual routing: 216,617 condition attempts to s2 and 72,208 to s3, exactly
  matching the intended 3:1 ratio
- Resource shift: s2 App CPU 36.31 seconds, s3 App CPU 29.26 seconds; total App
  CPU per condition 202 is about 10% lower than B11
- Decision: **current score and overall capacity/correctness champion**. It is
  fully backed up; test 2:1 once to close the measured CPU gap.

### B14: 2:1 edge/direct condition ingress (`aa78bde`)

- Score: 144,824, PASSED, deduction 0
- Accepted work: 250,251 condition 202, 948 registration 201, 26,206 trend 200
  and 22,288 condition-read 200
- Admission quality: condition 499 fell to 516; 98.1% of offered condition
  attempts succeeded, versus 96.9% in B13
- Resource balance: s2/s3 App CPU samples were 30.69/28.71 seconds, nearly
  equal; total App CPU fell 9.4% from B13
- Decision: retain as the CPU-balance and low-overload frontier. Because offered
  condition traffic was 11.6% lower, run an exact repeat before selecting the
  production ratio.

### B15: exact 2:1 repeat (`aa78bde`)

- Score: 150,001, PASSED, deduction 0
- Accepted work: 250,902 condition 202, 945 registration 201, 26,245 trend 200
  and 22,517 condition-read 200
- Failures: only 133 condition 499 and no condition 5xx
- Resource balance: s2/s3 App CPU 30.65/28.68 seconds, effectively identical
  to B14's 30.69/28.71
- Decision: 2:1 is a reproducible CPU-balance/low-overload frontier. Keep B13's
  3:1 as score/peak-ingest champion and use 2:1 to isolate the next code change.

### B16: initialize-time metadata registry (`0164d14`)

- Score: 155,390, PASSED, deduction 0
- Accepted work: 246,147 condition 202, 947 registration 201, 26,346 trend 200
  and 23,386 condition-read 200; only 111 condition 499
- Database effect: 46.59k -> 7.31k measured queries (-84.3%), 9s -> 2s DB
  execution; the former 26,140-call ownership/name query is gone
- Unit cost: s3 App CPU 28.68 -> 24.38 seconds (-15%) and total App CPU 59.33
  -> 54.67 seconds (-7.9%) versus the exact B15 base
- Correctness: initialize swaps a complete index generation; new metadata is
  published only after successful JIA activation and DB commit
- Decision: promote as a proven read/unit-cost frontier and test it with B13's
  score-champion 3:1 ingress ratio.

### B17: metadata registry with 3:1 ingress (`0164d14` + `d950610`)

- Score: 155,380, PASSED, deduction 0
- Accepted work: 262,184 condition 202, 910 registration 201, 25,385 trend 200
  and 23,645 condition-read 200; only 114 condition 499
- Database: 7.29k calls and 2s execution, preserving B16's 84% query reduction
- Unit cost versus old-code B13: total App CPU -8.1%; CPU per accepted condition
  about -1.8%
- Decision: retain as the 3:1 low-DB/low-failure frontier. Repeat once before
  choosing between the stable 2:1 and peak-ingest 3:1 bases.

### B18: exact registry plus 3:1 repeat (`0164d14` + `d950610`)

- Score: 144,949, PASSED, deduction 0
- Accepted work: 246,600 condition 202, 956 registration 201, 26,387 trend 200
  and 22,326 condition-read 200
- Comparable-load result: versus B16's 2:1 run, total App CPU +7.8% and
  condition reads -1,060 with nearly identical condition/registration/trend
- Decision: 2:1 becomes the stable base for the next code family. Preserve B13
  as exact score rollback and B17 as the higher-arrival 3:1 registry point.

### B19: direct compact condition decoder (`4471293`)

- Score: 149,153, PASSED, deduction 0
- Accepted work: 249,242 condition 202, 960 registration 201, 26,163 trend 200
  and 22,414 condition-read 200
- Offered load: 254,426 condition attempts versus 250,185 in B16; condition
  499 rose 111 -> 1,049 and p99 condition latency rose 172 -> 212 ms
- Unit cost: tracked successful work rose 296,826 -> 298,779 (+0.66%) while
  total App CPU fell 54.67 -> 52.61 seconds (-3.8%); approximate CPU per
  tracked success improved 4.4%
- Decision: retain as the decoder/unit-cost frontier even though scalar score
  fell 4.0%. The benchmark spent released capacity on additional condition
  attempts while the score-producing read mix fell, so score alone would give
  the wrong rollback decision.

### B20/B21: bounded fgprof observer (`d16ca58` in the toolkit)

- Scores: B20 149,862; B21 146,300; both PASSED with deduction 0
- Accepted condition work: 251,473 and **253,410**, versus B19's 249,242;
  tracked successful totals were 301,666 and 302,305 versus 298,779
- Failures: condition 499 was 63 and 132 versus B19's 1,049
- Observer cost: combined CPU-profile `runtime.gentraceback` fell from 5.07
  seconds to 2.57/2.49 seconds while both Apps retained a 15-second late-load
  fgprof, and every other evidence source remained 60 seconds
- Decision: promote the bounded observer as the measurement baseline. B20 is
  the lower-CPU point; B21 is the higher-ingest repeat. Their score difference
  is caused by read/workload mix and is not grounds to restore the expensive
  full-minute fgprof.

### B22/B23: pooled external condition bodies (`85955ce`)

- Scores: B22 141,421; B23 146,641; both PASSED with deduction 0
- B22 accepted **253,477** condition writes and **302,703** tracked successful
  requests, new frontiers; B23 accepted 252,440 and 301,984 with only 50
  condition 499s
- GC cost: combined `runtime.mallocgc` was 6.86/6.81 seconds versus 7.64/8.67
  for B20/B21 without the pool
- Total App CPU: 52.74/55.11 seconds versus 55.79/57.52 while accepted work
  stayed at or above the same level
- Decision: promote as a unit-cost/capacity improvement. B22's lower scalar
  score does not outweigh its best accepted-work total and lowest CPU per
  tracked success in this experiment family.

### B24/B25: in-place private batch encoder (`1d1b602`)

- Scores: B24 151,642; B25 152,634; both PASSED with deduction 0
- Accepted work: B24 259,296 condition / 308,479 tracked successes; B25
  **267,628 condition / 317,591 tracked successes**, new frontiers
- Tail and overload: condition p99 155/157 ms; condition 499 80/223
- Unit cost: total App CPU 54.24/57.08 seconds, approximately 0.176/0.180 ms
  per tracked success versus B23's 0.183 ms
- Allocation evidence: a fresh-process B24 profile is saved as
  `runs/20260720T124923.123912Z-s1-8181c5/{s2,s3}/allocs.pprof` and text
  summaries; it exposes the next private-response and synchronization costs
- Decision: promote as the current overall capacity frontier. Continue from
  it rather than selecting by scalar score alone.

### B26/B27: reusable private response state (`f6e566c`)

- Scores: B26 148,302; B27 142,847; both PASSED with deduction 0
- Tracked successful work: 304,944 and 301,523
- Unit cost: total App CPU 52.76/53.15 seconds, about **0.173/0.176 ms** per
  tracked success versus B24/B25's 0.176/0.180 ms
- Private path: B26 cumulative `forwardConditionBatch` CPU fell 3.83 -> 3.31
  seconds versus B24; the fixed status decoder itself is zero-allocation
- Decision: retain as the unit-cost frontier. Offered load was below B25, so
  the lower scalar scores do not justify restoring measured allocation waste.

### B28/B29: direct latest-condition reads (`ff2ef02`)

- Scores: B28 150,609; B29 147,456; both PASSED with deduction 0
- B28 work/cost: 305,194 tracked successes in 52.26 App CPU seconds, the best
  measured unit cost at approximately **0.171 ms/success**
- B29 workload: 295,049 tracked successes; despite the lower offered work,
  combined `mallocgc` fell to 5.90 seconds and trend-build CPU to 0.35 seconds
- Allocation proof: 512-history microbenchmark changed from about 36 us,
  68 KB and seven allocations to 7.6 us, zero bytes and zero allocations
- Decision: promote as read/unit-cost frontier. B28 is the comparable capacity
  point; B29 verifies the removed snapshot does not return under another mix.

### B30/B31: pooled private batch body (`fff8c99`)

- Scores: B30 151,197; B31 150,099; both PASSED with deduction 0
- Work/cost: B30 completed 308,123 tracked successes in 52.54 App CPU seconds;
  B31 completed 292,187 in 46.74 seconds, approximately 0.171 and **0.160
  ms/success**
- Local proof: a 64-item body changed from roughly 6--8 us, 33,856 B and 11
  allocations to 0.13 us, 66 B and two allocations; decoded retained strings
  do not alias the pooled storage
- Decision: promote the bounded body pool. It removes the 283 MB s3 allocation
  site identified in the fresh B24 profile and reduces s3 CPU at comparable
  work.

### B32/B33: one private batch sender (`4c57dc5`)

- Scores: B32 142,295; B33 149,955; both PASSED with deduction 0
- B31 eight-worker baseline: 163,952 updates / 158,277 private HTTP batches,
  average batch 1.036
- B32/B33 one-sender results: average batch 1.480/1.548 and tracked successes
  283,503/310,343 in 43.25/48.01 App CPU seconds, approximately **0.153/0.155
  ms/success**
- Safety: maximum observed batch was 64; B33 ended with only 13 queued items
  out of 65,536 and 72 condition 499s, with no new correctness deduction
- Decision: promote as the current unit-cost and transport-efficiency frontier.
  B33 exceeds B30's valid work while spending 8.6% less App CPU. Preserve B13
  independently as the scalar-score/final rollback candidate.

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
