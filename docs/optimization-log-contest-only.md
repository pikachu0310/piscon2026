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

## Four current-system maps

### Traffic

The live experiment is `benchmark -> s1 Nginx/TLS`, with `POST /api/isu`
routed to a registration-only App on s2 and every other API routed to the
authoritative App on s3. Both use MariaDB on s2. Static pages and assets
terminate on s1. Condition uploads still use `proxy_request_buffering off`, so
slow client bodies occupy both the s1 proxy stream and an s3 Go HTTP connection.

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

1. **Decode condition bodies on the spare s2 App.** Route only condition POST
   to s2, validate there, and forward a compact private representation to s3.
   Keep all reads and the authoritative generation state on s3. Compare total
   202 work, 499, per-node CPU, and s3 HTTP/decode cost before batching.
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

Hypothesis 1 produced B9's new score champion but overloaded s2 condition
ingress. The immediate follow-ups are to move registration back to s3, then
batch or balance the private condition hop. Hypotheses 2 and 3 remain
capacity-frontier work even if their first official score is flat or lower.

## Hourly checkpoints

Pending.
