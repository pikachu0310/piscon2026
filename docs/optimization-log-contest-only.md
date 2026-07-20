# PISCON contest-only optimization log

This ledger starts at the 2026-07-20 18:18:33 JST goal boundary. It uses only
the starting checkout, participant-visible materials, live state, and evidence
captured after this boundary. Do not import hypotheses or implementation details
from older repositories, pre-goal Git history, or `docs/optimization-log.md`.

## Session

- Start: 2026-07-20 18:18:33 JST
- Start commit: `e70329ffe14967d1959d528b056c31d1dd53a9c9`
- Branch: `codex/piscon-contest-only-20260720-1818`
- Benchmark duration: 60 seconds
- Starting live role declaration: s1=Nginx, s2=MariaDB, s3=Go App
- Evidence boundary: contest-distributed source/manual, live state, official
  PISCON result, and post-start measurements only

## Runs

| ID | Time JST | Commit | Hypothesis / bundle | Direct evidence | Benchmark / run | Score / validity | Frontier decision |
|---|---|---|---|---|---|---|---|
| B0 | 18:27 | `e70329f` | Unmodified starting checkout and live deployment | Fresh measure capture: access, slow log, CPU pprof, fgprof, pidstat and sar | portal `8c9bab42-1521-421b-8be1-23e77a008fea`; artifact `20260720T092734.057516Z-s1-a948bf` | **134,310**, PASSED, deduction 0 | Initial score champion |
| B1 | 18:42 | `891c84e` | Buffer complete condition request bodies at s1 before proxying to Go | Compare ingress status, handler wall, CPU, valid work and read latency with B0 | portal `c92807fa-0864-414d-8562-a3506f433a4b`; artifact `20260720T094228.582644Z-s1-af6c1c` | 131,466, PASSED, deduction 0 | Capacity result but not champion; keep commit, restore streaming for next isolated test |
| B2 | 18:50 | `367dd65` | Runtime-gzip the large client bundle on s1 | B0 transferred 979 MiB of vendor JS; compressed body is 211 KiB instead of 743 KiB | portal `78ca02d5-1e8d-456b-ac82-7d8e8a9240e1`; artifact `20260720T095009.878600Z-s1-4a0475` | 127,939, PASSED, deduction 0 | Mechanism valid, implementation rejected: runtime compression saturated ingress CPU |
| B3 | 18:55 | `da4fd9a` | Precompress immutable assets and serve with `gzip_static` | Same wire reduction as B2 without runtime compression | portal `af7892ca-48a7-4925-8247-dd5103111a1b`; artifact `20260720T095553.416750Z-s1-92e3b8` | 125,165, PASSED, deduction 0 | Capacity-frontier candidate; repeat before decision |
| B4 | 18:59 | `da4fd9a` | Exact repeat of B3 | Separate benchmark variance from static-compression mechanism | portal `926d9431-620d-4454-853d-4096d56eedba`; artifact `20260720T095959.518837Z-s1-a49d6d` | 128,296, PASSED, deduction 0 | Capacity frontier confirmed; not score champion |
| B5 | 19:20 | `f254b4c` | Compact generation-scoped condition state on the B3/B4 precompressed ingress | Heap proved 74% of retained memory was history; shrink each entry from 48 to 16 bytes and remove pointer scanning | portal `1d79911c-53b2-4613-8316-88e2b990697b`; artifact `20260720T102008.890588Z-s1-4fc66b` | **134,561**, PASSED, deduction 0 | New score champion and strong capacity frontier |
| B6 | 19:27 | `6ee2209` | Keep compact state but restore B0 uncompressed client delivery | B5 freed App capacity but precompression reduced registration arrivals; feed the frontier with B0 ingress behavior | portal `43a8ea3f-ffec-4e5e-bc1e-bb547653308b`; artifact `20260720T102755.490389Z-s1-57d2f2` | **142,430**, PASSED, deduction 0 | New score champion; compact state converts to score when demand is restored |
| B7 | 19:47 | `bb0d619` | Route only registration POSTs to a registration-only App colocated with MariaDB on s2 | Registration spends almost all wall time waiting for JIA while s2 is about 83% idle; isolate that wait from condition ownership on s3 | portal `58cf5dbc-634a-447e-bf6d-d3bb1829b98a`; artifact `20260720T104747.088972Z-s1-fd5739` | 134,195, PASSED, deduction 0 | Correct topology, not a work frontier on this run; repeat with both App profiles enabled |
| B8 | 19:53 | `bb0d619` | Exact B7 repeat after enabling s2 CPU pprof and fgprof capture | Distinguish benchmark/JIA variance and measure the actual registration-only process | portal `14708ae4-ef0c-40c5-b62d-2bead6c3cf86`; artifact `20260720T105336.406228Z-s1-53128e` | 136,587, PASSED, deduction 0 | Registration 201 exceeds B6, but condition 202 falls; retain as an isolation frontier, not score champion |
| B9 | 20:07 | `9d025e4` | Terminate external condition bodies on s2 and synchronously forward a compact private format to authoritative state on s3 | B8 left s2 81% idle while s3 spent 5.27 CPU-seconds and about 2,700 goroutine-seconds in condition handling/body reads | portal `f8ee8044-c525-4eaa-9a3d-a9af2cf51953`; artifact `20260720T110736.312178Z-s1-18f414` | **160,102**, PASSED, deduction 6 | New score champion, but deliberately overloaded edge is not the condition-capacity frontier; fix registration placement and synchronous fan-out |
| B10 | 20:13 | `e8d6467` | Keep the condition edge on s2 but route registration back to newly freed s3 | B9 colocated registration with the saturated edge and produced 779 registration 499s plus seven 500/502 responses | portal `40713158-859f-4ba4-a1bb-dc2cb68def5d`; artifact `20260720T111323.749645Z-s1-816101` | 147,446, PASSED, deduction 0 | Stronger total-work/correctness frontier: 999 registrations and more condition 202, but less read work than B9; batch the private hop next |
| B11 | 20:20 | `3f0fc62` | Keep B10 routing and batch up to 64 already-queued compact condition updates behind eight edge workers | B10 spent 52.98 edge CPU-seconds while synchronously forwarding each accepted body and returned about 118k condition 499s | portal `cf2f1369-52fb-43a3-9965-26b1fcc7cf24`; artifact `20260720T112047.518093Z-s1-cc4808` | **156,224**, PASSED, deduction 0 | New overall capacity/correctness champion: near-score-champion result while accepting 2.41x B9 condition writes at much lower CPU cost |
| B12 | 20:34 | `eeafca4` intent, B11 effective | Intended 3:1 split of condition ingress across s2 and s3 | Validate deployment from captured `upstream_addr`, not only repository diff and `nginx -t` | portal `dd5e2228-86d9-496a-afa1-6416f3bce7ed`; artifact `20260720T113448.928674Z-s1-0e9c09` | **151,449**, PASSED, deduction 0 | Valid B11 repeat and new condition-ingest count frontier; not a split experiment because all 261,883 condition attempts still reached s2 |
| B13 | 20:42 | `eeafca4` effective | Send condition POSTs 3:1 to the batched s2 edge and directly to authoritative s3 | B11/B12 left edge CPU above authoritative CPU; every route still updates the sole s3 generation | portal `04b6a6bd-0294-474c-9424-37a5c0603e9a`; artifact `20260720T114250.931832Z-s1-c1eddf` | **162,980**, PASSED, deduction 0 | New scalar, condition-ingest, and balanced-capacity champion; test 2:1 to close the remaining CPU gap |
| B14 | 20:49 | `aa78bde` | Change condition ingress from 3:1 to 2:1 edge:direct | B13 App CPU samples were 36.31s on s2 and 29.26s on s3 | portal `fd30ddd0-6e24-4eb7-8187-44779e45b15c`; artifact `20260720T114923.209743Z-s1-f2e80d` | **144,824**, PASSED, deduction 0 | CPU-balance and low-overload frontier, not an automatic regression; offered condition load was 11.6% lower, so repeat exactly |
| B15 | 20:53 | `aa78bde` | Exact repeat of the 2:1 edge:direct split | Separate ratio behavior from B14's offered-load trajectory | portal `fe92aa0a-306f-4300-b958-cc61bea2c383`; artifact `20260720T115346.002992Z-s1-8fcb16` | **150,001**, PASSED, deduction 0 | Confirms a stable CPU-balance/low-failure frontier; use it as the isolation base for the metadata registry |
| B16 | 21:00 | `0164d14` App + `aa78bde` Nginx | Build an initialize-time ISU metadata registry and publish registrations after commit | B13 slow log: 46.59k queries; 26,140 repeated ownership/name SELECTs used 3.85s | portal `bd59ce1c-e5ca-481a-94f4-248094d2b494`; artifact `20260720T120057.641902Z-s1-f0a807` | **155,390**, PASSED, deduction 0 | Proven unit-cost/read-path improvement on stable 2:1 base; combine with B13's 3:1 ratio next |
| B17 | 21:07 | `0164d14` App + `d950610` Nginx | Combine the metadata registry with B13's 3:1 ingress ratio | B16 proves the registry saving; B13 is the score/peak-ingest ratio | portal `96b580d0-3bce-4d40-aaab-d1e9006b9741`; artifact `20260720T120724.815614Z-s1-843bcc` | **155,380**, PASSED, deduction 0 | Strong 3:1 low-DB/low-499 frontier, not score champion; exact repeat before choosing the ratio |
| B18 | 21:11 | `0164d14` App + `d950610` Nginx | Exact repeat of registry plus 3:1 ingress | Determine whether B17 converts registry savings as reliably as the 2:1 reference | portal `0c194caf-83bf-451e-bd6d-938857808d76`; artifact `20260720T121118.177737Z-s1-697e02` | **144,949**, PASSED, deduction 0 | Similar accepted load to B16 at higher CPU and fewer reads; select 2:1 as the stable isolation base while preserving B13 rollback |

### B0 facts

- Condition ingestion dominated traffic: 250,829 attempts, 243,741 HTTP 202,
  3,943 HTTP 400, 3,073 HTTP 499 and 72 HTTP 404. The 400s carried only
  8.5 bytes on average and are malformed probes, not capacity failures. Successful requests
  covered 850 UUIDs and carried 353.9 MiB of request data.
- Successful condition request time was 9.92 ms average, 60 ms p95 and
  93 ms p99. Attempts rose from about 1.3k/s to 6-7k/s. Partial/cancelled
  uploads increased late in the run; the meaningful overload signal is the
  499 group, which averaged 106 ms and reached 3.9% in the final five seconds.
- Registration produced 893 HTTP 201 responses. It averaged 242 ms with a
  603 ms p95. Four requests ended as 499.
- App CPU pprof sampled 37.03 CPU-seconds. `postIsuCondition` accounted for
  7.23 cumulative seconds; allocation, GC scanning, JSON decode and HTTP
  syscall/runtime work dominate its descendants. fgprof observed 2,731
  goroutine-seconds in `io.ReadAll` for condition bodies and 203 seconds in
  registration's external HTTP client.
- s3 App host averaged 47.5% busy across two CPUs (32.75% user, 14.77%
  system), while s2 DB averaged 83.66% idle. The DB executed 2,750 measured
  queries in the logged 23-second interval, only 448 ms total execution and
  116 ms total lock time. It is not the B0 saturation point.
- DB metadata work is nevertheless a capacity-frontier candidate: metadata
  SELECTs were 2,178/2,750 queries; list/trend scans were 191.6k/193.6k rows
  examined and trend metadata returned about 90% of DB response bytes.

### B1 decision: buffering isolated App work but hurt admission

- Official score moved -2.1%, registration success 893 -> 876 (-1.9%), and
  condition 202 count 243,741 -> 228,059 (-6.4%). Condition 499 increased
  3,073 -> 4,319; aborted uploads contained only 38 bytes on average.
- On the positive side, CPU samples fell 37.03 -> 31.59 seconds (-14.7%), or
  about 9% less total App CPU per successful condition. The slow body wait
  disappeared from `postIsuCondition`, read endpoint tail latency collapsed,
  and trend average fell from 6.7 ms to 1.9 ms.
- This proves an ingress-isolation mechanism but does not convert to more
  accepted condition work: buffering makes slow uploaders hit their client
  deadline before proxying. It is recorded as a capacity result, not selected
  as score champion. Streaming is restored before testing static compression.

### B2 decision: bandwidth fell, but runtime gzip moved the bottleneck to s1

- Vendor responses shrank from about 743 KiB to 211 KiB, proving clients use
  gzip. However s1 CPU became 63.8% busy across two CPUs and both Nginx workers
  commonly consumed 40-90% of a CPU each.
- Condition offered attempts rose to 263,690, but 31,870 ended as 499. Valid
  condition 202 fell to 227,942, registration success fell to 781, and every
  proxied endpoint developed a large latency tail.
- The bandwidth mechanism is retained, but compression must move outside the
  benchmark. B3 precompresses immutable assets once and uses `gzip_static`.

### B3/B4 decision: same condition work at lower unit cost

- B3/B4 scores were 125,165 and 128,296, below B0's 134,310.
- B4 nevertheless completed 242,988 condition 202 responses versus B0's
  243,741 (-0.3%), while condition 499 fell 3,073 -> 2,296 and App CPU samples
  fell 37.03 -> 34.31 seconds (-7.3%). App CPU per successful condition fell
  about 7%.
- Registration success was lower (868 versus 893), so the saved work has not
  converted to score. Precompression remains a capacity frontier and is the
  base for downstream App allocation/state work; B0 remains score champion.
- A post-run heap profile made the next constraint concrete: 191.7 MiB in use,
  of which 142.4 MiB (74.3%) was `cacheConditionHistory` backing storage and
  29.2 MiB was live `io.ReadAll` body buffers. The next structural family is a
  compact generation-scoped condition representation plus streaming decode.

### B5 decision: compact state removed the GC/memory wall

- The internal condition entry is now 16 bytes: timestamp, message ID and four
  flag bits. Canonical strings and messages live once in generation-owned
  tables. Histories, message table and trend cache swap together at initialize.
- In-use heap fell 191.7 -> 73.5 MiB (-61.7%). History backing storage fell
  142.4 -> 42.2 MiB (-70.4%). CPU samples fell 37.03 -> 27.88 seconds
  (-24.7%), `scanobject` 3.28 -> 1.28 seconds and `mallocgc` 4.90 -> 3.07.
- Condition 499 fell 3,073 -> 71. Official score reached 134,561, narrowly
  above B0, even though condition 202 and registration counts were lower.
- This is both score champion and a much stronger capacity platform. B6 removes
  precompression while keeping compact state to feed the saved capacity with
  the B0 client-arrival behavior.

### B6 decision: demand plus compact capacity converted to score

- Registration success reached 896 (B0 893) and condition 202 reached 246,225
  (B0 243,741). Condition 499 remained only 544 versus B0's 3,073.
- Official score rose to 142,430 (+6.0% over B0). App CPU was 32.26 seconds
  while doing more valid work; history remained compact.
- Compression was not the useful production setting on this workload: network
  was not the gating resource and compressed client handling reduced arrivals.
  B6's uncompressed ingress plus compact state becomes the score champion.
- The next structural family is node topology: move registration's external
  wait and image body retention to idle s2 while keeping condition ownership on
  s3, with DB fallback on a known-UUID cache miss.

### B7/B8 decision: registration isolation is real but not yet converted

- Method routing is exact: `POST /api/isu` reaches s2, while `GET /api/isu`,
  initialize, condition reads/writes and trend remain on s3. A shared-DB
  positive read-through prevents newly registered UUIDs from remaining 404.
- B8 completed 901 registrations versus B6's 896. The registration-only App
  used only 3.25 CPU-seconds, while fgprof attributed 198.24 goroutine-seconds
  to its JIA HTTP call. The isolated work is overwhelmingly waiting, not CPU.
- B8 condition 202 fell 246,225 -> 244,519 and score fell 142,430 -> 136,587.
  This is not labeled an automatic regression: one more piece of accepted work
  can shift pressure downstream. Here the registration increase is only five,
  however, and total accepted condition work also fell, so the run does not
  establish a larger total-work frontier by itself.
- Condition 499 remained low (99 versus B6's 544), and s3 busy CPU fell from
  43.1% to 40.5%. The topology therefore remains useful isolation machinery.
  The next conversion attempt moves condition body/decode work to the same
  spare process while preserving the authoritative compact state on s3.

### B9 decision: freeing reads raised score while write capacity fell

- s2 reads and validates the original JSON condition body, replaces the long
  canonical condition string with four flag bits, and sends a private binary
  request to s3. s3 remains the only owner of histories, messages and trend;
  all reads therefore retain immediate access to one authoritative generation.
- Score rose 142,430 -> 160,102 (+12.4%). Read work increased materially:
  trend 200 responses rose 24,421 -> 29,611 and condition reads 21,551 ->
  23,270. The main App's CPU samples fell 32.26 -> 26.08 seconds.
- This was not a general throughput win. Condition 202 fell 246,225 -> 100,209
  while 499 rose 544 -> 117,143. Registration 201 fell 896 -> 692, with 779
  registration 499s plus seven 500/502 responses. s2 became 67.7% busy and its
  App sampled 53.31 CPU-seconds.
- fgprof itself became expensive on the overloaded edge: profiling tens of
  thousands of goroutines put 12.65 CPU-seconds under stack collection, while
  condition JSON decode used 4.26 seconds and synchronous forwarding 5.40.
- This is the concrete reason not to classify solely from a scalar score.
  B9 is the score champion because the benchmark rewarded the read capacity it
  unlocked, while B6 remains the stronger condition-ingest frontier. The next
  isolated change routes registration back to the freed s3 App; after that,
  balance or batch condition forwarding instead of accepting the 499 wall.

### B10 decision: lower score, much healthier registration capacity

- Moving only registration back to s3 raised HTTP 201 from 692 to 999 and
  removed every registration 499/500/502. Condition 202 also rose 100,209 ->
  110,927. This is a structural improvement despite the score falling.
- Score fell 160,102 -> 147,446 because successful reads fell: trend 29,611 ->
  27,722 and condition reads 23,270 -> 22,986. The benchmark shifted work
  toward newly admitted registrations and their downstream condition traffic.
- The synchronous condition edge remains overloaded: about 118k condition
  499s and 125 condition 500/502s; s2 stayed about 67.5% busy and its App used
  52.98 sampled CPU-seconds. B10 is the current total-work/correctness frontier,
  while B9 remains the scalar score champion.
- B11 keeps B10 routing and batches up to 64 compact updates behind eight
  internal workers. It does not delay lightly loaded requests; it only merges
  work already queued under pressure.

### B11 decision: batching converted spare-node isolation into real capacity

- Official score reached 156,224, only 2.4% below B9's scalar champion, with
  deduction 0. The final 38 score points lost to 383 timed-out checks are kept
  separate from correctness deductions.
- Accepted condition writes rose from 100,209 in B9 and 110,927 in B10 to
  **241,858**: 2.41x B9 and 2.18x B10. Condition 499 fell from 118,419 in B10
  to 7,334. Registration remained healthy at 936 HTTP 201 responses, with only
  four registration 499s and no registration 500/502.
- The work increase did not come from spending more CPU. Edge App CPU samples
  fell 52.98 -> 41.16 seconds and authoritative App CPU fell 30.38 -> 21.85
  seconds. Total sampled App CPU fell 83.36 -> 63.01 seconds (-24.4%) while
  accepted condition writes more than doubled. Edge CPU per accepted condition
  fell from about 0.478 ms to 0.170 ms (-64.4%).
- Successful reads also remained substantial: 26,025 trend reads and 23,440
  condition reads, versus B9's 29,611 and 23,270. B11 therefore is not merely
  shifting all resources from reads to writes; it is the strongest balanced
  result so far.
- This run demonstrates why a lower score is not automatically a regression.
  B9 remains the scalar score champion at 160,102, while B11 is promoted to the
  overall capacity/correctness champion and the base for subsequent work.

### B12 decision: a useful repeat exposed a deployment false assumption

- B12 accepted 245,775 condition writes, 944 registrations, 25,582 trend reads
  and 24,731 condition reads. Its 151,449 score is lower than B11, but the run
  independently reproduces the batched edge's high write capacity and slightly
  exceeds B11's condition 202 count.
- The intended weighted split did not execute. Every one of 261,883 condition
  attempts had `upstream_addr=10.0.0.143:3000`. s2 and s3 CPU consequently
  stayed close to B11 at 41.95 and 24.14 sampled seconds.
- The cause was operational, not algorithmic: the active
  `/etc/nginx/sites-enabled/isucondition.conf` is a regular file rather than a
  symlink. Deploying only the
  `sites-available` copy passed `nginx -t` and the generic doctor checks but did
  not change the loaded server block.
- The active file is now backed up with the B11 champion, both copies receive
  the weighted config, and `nginx -T` plus eight loopback requests confirm an
  exact 6:2 s2:s3 split. The first actual split run is B13.

### B13 decision: balanced ingress joined score and capacity frontiers

- The weighted upstream sent 216,617 attempts to s2 and 72,208 directly to s3,
  an exact 3:1 split. Both routes update the single authoritative generation on
  s3, so reads retain immediate consistency.
- Official score reached **162,980**, PASSED with deduction 0, exceeding B9's
  former 160,102 score champion. At the same time, accepted condition writes
  reached **279,985**, 15.8% above B11 and 2.79x B9. Condition 499 fell to
  5,641. Registration remained healthy at 900 successes and three 499s.
- Read work remained competitive: condition reads rose to 26,468, though trend
  reads fell to 23,999. This result is not built solely by shedding writes in
  favor of reads as B9 was.
- Edge App CPU fell 41.16 -> 36.31 sampled seconds while s3 rose 21.85 -> 29.26
  as intended. Total App CPU per accepted condition fell about 10% from B11,
  even though the total CPU sample count rose slightly with much more work.
- B13 is the first configuration to hold the scalar score, condition-ingest,
  and balanced-capacity frontiers simultaneously. It is backed up on all three
  servers under `/home/isucon/score-champion-b13`. A 2:1 edge:direct split is
  the next bounded experiment because it should bring the two App CPU samples
  close to equal.

### B14 decision: score fell while balance and admission quality improved

- The measured routing was 170,108 attempts through s2 and 85,062 directly to
  s3, exactly 2:1. App CPU samples converged from B13's 36.31:29.26 seconds to
  30.69:28.71 seconds, and total App CPU fell 9.4%.
- Score fell to 144,824 and condition 202 fell to 250,251. This is not enough
  evidence to reject the ratio: total condition attempts also fell 288,825 ->
  255,170 (-11.6%), while the successful share increased from 96.9% to 98.1%.
- Condition 499 fell 5,641 -> **516**, registration success rose 900 -> 948,
  and registration had only four 499s. B14 is the cleanest overload/correctness
  result so far, even though the particular offered-load trajectory produced a
  lower scalar score.
- Total App CPU per accepted condition was approximately flat versus B13
  (0.237 ms versus 0.234 ms). Repeat the exact 2:1 configuration in B15 to
  distinguish ratio behavior from benchmark demand variance.

### B15 decision: the 2:1 capacity behavior is reproducible

- B15 repeated 250,902 condition 202, 945 registration 201, 26,245 trend reads
  and 22,517 condition reads. These are very close to B14's 250,251, 948,
  26,206 and 22,288 despite a different official score of 150,001.
- Condition 499 fell even further from 516 to **133**. s2/s3 App CPU samples
  repeated at 30.65/28.68 seconds versus B14's 30.69/28.71. This is strong
  evidence that 2:1 reliably balances compute and admits almost every offered
  condition request.
- B13's 3:1 remains the score and peak-ingest champion; B14/B15 establish 2:1
  as the stable low-overload frontier. The next structural family uses 2:1 as
  an isolation base: replace repeated public-read metadata SELECTs with an
  initialize-time registry, then combine a proven win with the B13 ratio.

### B16 decision: the metadata registry removed the measured SQL wall

- Initialize now loads owner/name/character/ID metadata into one indexed
  generation. List, detail, graph authorization, condition authorization/name
  and trend reads use that registry. A successful registration is published to
  it only after the MariaDB transaction commits; MariaDB remains durable truth.
- Slow-log volume fell from B13's 46.59k queries to **7.31k** (-84.3%), unique
  query shapes fell 17 -> 12 and measured DB execution fell 9s -> 2s. The
  former 26,140-call ownership/name SELECT disappeared completely.
- On the stable 2:1 base, s3 App CPU fell 28.68 -> 24.38 sampled seconds (-15%)
  from B15; total sampled App CPU fell 59.33 -> 54.67 seconds (-7.9%). s2 App
  CPU remained essentially flat at 30.29 seconds, locating the saving exactly
  on the metadata-reading node.
- Score rose 150,001 -> 155,390. The run still completed 246,147 condition
  writes, 947 registrations, 26,346 trend reads and 23,386 condition reads;
  condition 499 was only 111. This is a real unit-cost/read-capacity improvement,
  not a score-only workload shift.
- B17 restores B13's 3:1 score/peak-ingest ratio while keeping the registry.

### B17 decision: registry savings persisted, score conversion varied

- The active 3:1 split was exact: 199,854 condition attempts through s2 and
  66,619 directly to s3. It completed 262,184 condition writes with only 114
  499s, plus 910 registrations, 25,385 trend reads and 23,645 condition reads.
- The registry effect persisted: 7.29k DB calls and 2s measured execution,
  essentially identical to B16. Versus the old-code B13 3:1 run, total App CPU
  fell 65.57 -> 60.28 seconds (-8.1%) and CPU per accepted condition improved
  about 1.8%.
- Score remained 155,380 rather than B13's 162,980 because the work mix differed:
  accepted condition work was 6.4% lower, condition reads 10.7% lower and the
  benchmark admitted a different registration/trend trajectory. The code change
  did not reintroduce failures or DB cost.
- Retain B17 as a 3:1 low-DB/low-overload capacity frontier. B18 repeats it
  exactly before selecting the downstream base.

### B18 decision: 2:1 is the better stable base after indexing

- B18 completed 246,600 condition writes, 956 registrations, 26,387 trend
  reads and 22,326 condition reads with 308 condition 499s. These condition,
  registration and trend counts closely match B16's 2:1 workload.
- Under that comparable load, total App CPU was 58.95 seconds versus B16's
  54.67 (+7.8%), and condition reads were 1,060 lower. Score was 144,949 versus
  155,390. B17's higher condition arrival was not reproduced.
- The result is useful even though it does not raise score: it controls for
  offered work and shows that 3:1 consumes more edge CPU without consistently
  turning it into downstream reads. Restore 2:1 for the next isolated code
  family. Keep the exact B13 3:1 backup as the scalar-score rollback target.

### B19 decision: lower score, but cheaper decoding admitted more work

- The condition decoder now fills the compact forwarding representation
  directly instead of first building the larger request model and converting
  it. The 2:1 routing base was otherwise unchanged.
- Score fell from B16's 155,390 to 149,153, but successful condition writes
  rose 246,147 -> **249,242** and the four tracked successful request families
  rose 296,826 -> **298,779** (+0.66%). Registration success also rose 947 ->
  960.
- At the same time, total sampled App CPU fell 54.67 -> **52.61 seconds**
  (-3.8%). Approximate CPU per tracked successful request therefore improved
  from 0.184 ms to 0.176 ms (-4.4%). This is retained as a unit-cost frontier,
  despite the scalar-score regression.
- The newly available capacity was consumed by more offered condition work:
  attempts rose 250,185 -> 254,426, condition 499 rose 111 -> 1,049 and p99
  condition latency rose 172 -> 212 ms. Read mix also moved: trend reads fell
  183 and condition reads fell 972. Those shifts explain why cheaper decoding
  did not immediately convert into score.
- This run is the concrete warning against score-only rollback. Preserve the
  direct decoder and next reduce measurement/GC overhead or feed the released
  capacity into useful reads; do not undo it merely because one official run
  scored 4.0% lower.

### B20/B21 decision: keep full evidence, bound the expensive observer

- CPU pprof, access/slow logs, sar and pidstat still cover the complete 60
  seconds. Only fgprof changed from a full-minute capture to a representative
  15-second window beginning at second 40; the exact window is recorded in
  every host's `meta.json`.
- B20 scored 149,862 and completed 251,473 condition writes; B21 scored 146,300
  and completed **253,410**. Their four tracked successful request totals were
  301,666 and 302,305, both above B19's 298,779.
- Condition 499 fell from B19's 1,049 to 63 and 132. CPU-profile
  `runtime.gentraceback` across both Apps fell from 5.07 seconds to 2.57 and
  2.49 seconds. Both runs retained useful 15-second fgprof files and had zero
  capture errors.
- B20's total App CPU was 55.79 seconds and B21's was 57.52 as B21 admitted
  more condition work. The exact repeat therefore exposes workload variance,
  but it also reproduces the observer-specific saving and healthy admission.
- Promote the bounded window as the measurement baseline. This is not hiding
  diagnostics to inflate score: the expensive wall profiler remains present
  at peak load, while the other four evidence streams remain full-duration.

### B22/B23 decision: pooled request bodies lower GC while work expands

- Measured condition requests were about 1.5 KiB and below 2 KiB. The public
  condition handler now reads known-size bodies once into a 2 KiB pooled
  buffer; unknown-size or larger bodies keep the old unbounded fallback.
  Tests prove decoded messages do not alias a returned buffer.
- The isolated microbenchmark moved body reading from about 456 ns, 1,472 B
  and four allocations to about 72 ns, 64 B and two allocations. Normal tests,
  race tests, truncated-body handling and fallback handling all passed.
- B22 scored 141,421 while setting a tracked-success frontier of **302,703**
  and condition-write frontier of **253,477**. B23 scored 146,641 with 301,984
  tracked successes and only 50 condition 499s. Both were PASSED with deduction
  0.
- Combined `runtime.mallocgc` fell to 6.86/6.81 seconds versus 7.64/8.67 in
  the two no-pool baselines. Total App CPU was 52.74/55.11 seconds versus
  55.79/57.52. The exact repeat therefore confirms lower GC and CPU across
  different offered/read mixes.
- Keep the body pool as a unit-cost and capacity improvement. B22 is another
  direct example where a 141k score run is stronger on accepted work and CPU
  efficiency than higher-scoring runs.

### B24/B25 decision: direct batch encoding opens a new capacity tier

- The edge previously encoded every request into a temporary private payload,
  then copied all payloads into the final batch. The new two-pass encoder sizes
  the complete batch and writes each request directly into that one buffer.
  Byte-for-byte wire compatibility and race tests passed.
- A 64-request microbenchmark moved from roughly 8.0 us, 20,480 B and 66
  allocations to 4.2 us, 9,472 B and one allocation.
- B24 scored 151,642 with 259,296 condition writes and 308,479 tracked
  successes. B25 scored 152,634 with **267,628** condition writes and
  **317,591** tracked successes. Both are new work frontiers, PASSED and
  deduction 0; condition p99 was 155/157 ms.
- B24 total App CPU was 54.24 seconds, lower than B23's 55.11 while accepting
  6,495 more tracked successes. B25 used 57.08 seconds for another 9,112
  successes. CPU per tracked success remained about 0.176/0.180 ms versus
  B23's 0.183 ms.
- A post-B24 allocation profile, taken after a fresh deploy and one official
  run, is stored beside the run. It attributes 97 MB on s2 to the now-single
  final batch buffer, but also reveals 736 MB cumulative allocation below the
  private HTTP path, 84 MB in tiny response `ReadAll`, and 27 MB in per-request
  result/channel state. These become the next isolated target.

### B26/B27 decision: reuse private response and synchronization state

- The eight-worker transport and wire format stayed fixed. Each edge request
  now reuses its result channel, reads the at-most-134-byte private response
  into a stack buffer, and decodes statuses into the existing result slice.
- The status decoder microbenchmark moved from about 190 ns, 512 B and one
  allocation to about 70 ns with zero bytes and zero allocations. Normal and
  race tests passed.
- B26 scored 148,302 with 304,944 tracked successes and 52.76 App CPU seconds,
  the best CPU per tracked success in the family at about **0.173 ms**. Private
  hop cumulative CPU fell from B24's 3.83 to 3.31 seconds.
- B27 scored 142,847 with 301,523 tracked successes and 53.15 CPU seconds,
  about 0.176 ms per success. Its offered/error mix was weaker, but unit cost
  remained equal to B24 and below B25. Both runs were PASSED with deduction 0.
- Retain the allocation removal. The next authoritative profile target is the
  143 MB full-history/latest snapshot plus 19.5 MB metadata snapshot used by
  list/trend reads.

## Four current-system maps

### Traffic

The live B14 experiment is `benchmark -> s1 Nginx/TLS`. Condition POST bodies
are weighted 2:1 between the edge App on s2 and authoritative App on s3. The s2
path decodes and batches up to 64 already-queued compact updates to s3; the s3
path updates that same state directly. Registration and every public read
execute on s3; both Apps use MariaDB on s2. Static pages and assets terminate
on s1.

### State ownership

- MariaDB: users, ISU metadata/owner/character/icon, JIA URL and seed data.
- Go process memory: scoring-run condition history, known UUIDs, latest values
  derived from history, sessions/icons caches and the 100 ms trend cache.
- A condition POST is never persisted during scoring. Therefore ordinary
  round-robin App replication would violate read-after-write consistency.
- `conditionMessageInterner` and `isuRegistrationLocks` survive initialize;
  their run-to-run heap growth must be measured and then removed.

### Score-critical path

1. Registration opens a DB transaction, inserts an image, waits for JIA
   activation, commits, then publishes the UUID as known.
2. JIA sends streamed condition bodies through s1 to the only App process.
3. The App reads the complete body, decodes JSON and appends to UUID memory.
4. Read/trend requests repeatedly derive results from global memory and remote
   metadata. Increasing successful registrations can lower the immediate
   score by moving saturation into steps 2-4; that is evaluated as a capacity
   frontier, not an automatic regression.

### Three-node resources

- All nodes: c5.large, 2 vCPU, about 3.7 GiB RAM, no swap.
- s1: Nginx ingress/static only; spare application CPU is available.
- s2: MariaDB; B0 average 83.66% idle, disk utilization 6.87%.
- s3: only App; B0 average 47.5% CPU busy with substantial HTTP/body and GC
  work. Legacy local mysqld processes remain on s1/s3 but are not used by the
  configured App.

## Current hypothesis queue

1. **Balance condition ingress between s2 and s3.** B11 made the compact edge
   efficient, but s2 is still the busiest App node. Test a stable traffic split
   while every path continues to update the authoritative state on s3.
2. **Build an initialize-time `IsuRegistry`.** Replace owner/name/list/id/
   character metadata SQL with immutable memory indexes, publishing a new ISU
   only after JIA success and DB commit.
3. **Maintain condition-derived indexes at ingest.** Store compact level bits,
   per-UUID latest and hourly/trend aggregates instead of rescanning strings
   and complete histories on each read.
4. **Make registration an explicit pending state machine.** Move external JIA
   I/O outside the DB transaction and buffer early conditions until successful
   publication.
5. **Shard UUID owners behind s1.** Route condition write/read/graph by stable
   UUID to App workers on spare nodes and aggregate only global latest/metadata
   summaries. Do not use random load balancing.

The compact condition-edge family produced B9's score champion, B10's
registration recovery, and B11's overall capacity champion. B11 resolves the
per-request private-hop fan-out; the next isolated topology test balances the
remaining edge pressure. Hypotheses 2 and 3 remain capacity-frontier work even
if their first official score is flat or lower.

## Hourly checkpoints

### 20:21 JST

- Scalar score champion: B9, 160,102. It favors successful reads while shedding
  most offered condition writes, so it is not treated as the best system in
  every dimension.
- Capacity/correctness champion: B11, 156,224. It accepts 241,858 condition
  writes, 936 registrations, 26,025 trend reads and 23,440 condition reads with
  no correctness deduction.
- Evaluation rule from this point: track score, accepted work mix, failure
  classes, tail latency, and resource cost together. A score drop accompanied
  by more valid downstream work or lower unit cost remains a retained frontier;
  it receives follow-up experiments rather than an automatic rollback.

### 20:44 JST

- B13 unifies the frontiers at 162,980 score, 279,985 accepted condition writes
  and deduction 0. Its measured 3:1 ingress split also lowers CPU per accepted
  condition by about 10% from B11.
- The remaining topology signal is an s2:s3 App CPU sample ratio of 36.31:29.26.
  Test 2:1 next, then stop tuning this ratio if score/work/unit-cost do not move
  together and return to the metadata/state-index structural families.

### 21:02 JST

- B14/B15 established a stable 2:1 reference. B16's isolated metadata registry
  cut database calls 84%, s3 App CPU 15% and total App CPU 8%, while raising
  score and preserving valid work.
- The registry family is promoted. Convert it with the 3:1 champion ratio next;
  then profile the new system instead of continuing to tune removed SQL calls.

### 21:27 JST

- B19 demonstrates the multi-axis rule under live load. Direct compact decoding
  reduced total App CPU 3.8% while increasing tracked successful work 0.66%,
  even though score fell to 149,153 as condition overload and read mix moved.
- Keep the decoder as a unit-cost frontier. The next experiments target costs
  visible in the new profile, with an exact repeat whenever offered-load
  variance could otherwise dominate the conclusion.

### 21:36 JST

- B20/B21 validate a late 15-second fgprof window. Both runs process more valid
  work than B19, keep complete CPU/access/slow/OS evidence, and halve measured
  stack-walk CPU while retaining useful off-CPU profiles.
- The new profile now attributes 2.18 seconds on s2 and 1.58 seconds on s3 to
  request-body `io.ReadAll`. Measured condition requests are about 1.5 KiB and
  stay below 2 KiB, so a bounded pooled-body experiment is next.

### 21:47 JST

- B22/B23 confirm that bounded request-body pooling reduces GC and App CPU
  while preserving or increasing accepted work. Promote commit `85955ce`.
- The remaining s2 profile spends 3.76 seconds in the private forwarding path.
  Its encoder currently allocates one temporary payload per request and then
  copies those payloads into a batch. Direct final-buffer encoding is the next
  isolated change.

### 21:57 JST

- B24/B25 promote commit `1d1b602`: accepted condition and total successful
  work rose sharply in both runs while unit CPU cost improved.
- Saved allocation profiles identify private-response buffering and
  per-request synchronization objects as the next removable costs. Keep batch
  worker count at eight for that experiment so transport concurrency is not
  confounded with allocation removal.

### 22:07 JST

- B26/B27 retain commit `f6e566c`; B26 establishes the best measured unit CPU
  cost even though neither run receives B25's high offered-load trajectory.
- List/trend currently copy every history and registry trend row before
  selecting latest values. Replace those snapshots with protected direct
  lookups, keeping response ordering and registry/state ownership unchanged.
