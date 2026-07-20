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

## Four current-system maps

### Traffic

`benchmark -> s1 Nginx/TLS -> s3 Go App -> s2 MariaDB`. Static pages and
assets terminate on s1. Every API request goes to the single s3 App. Condition
uploads use `proxy_request_buffering off`, so slow client bodies occupy both
the s1 proxy stream and an s3 Go HTTP connection.

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

1. **Buffer small condition bodies at Nginx.** With request buffering on, s1
   absorbs slow uploads and forwards a complete in-memory body to Go. Test for
   lower Go goroutine wall time, HTTP syscall/GC cost and fewer late 400/499.
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

The first structural test is hypothesis 1 because B0 directly measured 2,731
goroutine-seconds waiting in body reads and growing late-run partial/cancelled
uploads. Hypotheses 2 and 3 remain capacity-frontier work even if their first
official score is flat or lower.

## Hourly checkpoints

Pending.
