# PISCON 2026 final manifest（2026-07-22）

このページは、古い実験台帳とは別に、2026-07-22時点のscore championとfinal構成を一意に選ぶための正本です。

## Official score champion

- benchmark ID: `417c71ac-4739-4788-ba23-c9d934252269`
- result: **6,379,010 / PASSED / deduction 0 / timeout 0**
- App source: `8dc37724d32521bc16eef3799f5725fcbaaff54c`
- s2/s3 App binary SHA-256: `9f169b88712a876496f39097e41f03b4164395fe08f63834f48e917f5c1e4121`
- capture: `20260722T062148.370372Z-s1-2ef6c1`

`docs/score-frontiers.md`のB13（162,980点）と`docs/optimization-log.md`のE54 final（139,772点）は過去の履歴であり、rollback先ではありません。

## Git state

- branch: `codex/piscon-score-growth-win-20260722`
- branch HEAD: `773beeb7fb99815a945370144c4ae29d9ce55525`
- score-proven App commit: `8dc37724d32521bc16eef3799f5725fcbaaff54c`
- `773beeb`の差分はSPA shell用Nginx client cacheとR4台帳です。R4の6,379,010点を得たNginx siteそのものとは区別します。

## Three-host topology

| Host | Private IP | Active role |
|---|---|---|
| s1 | `10.0.0.26` | public Nginx、static、API routing |
| s2 | `10.0.0.143` | MariaDB、condition edge/batch forward、initialize gate helper |
| s3 | `10.0.0.113` | authoritative condition state、registration、initialize、全read API |

condition POSTはs2:s3へ2:1で振り分け、s2が受理したcompact batchもs3へ転送します。POST `/api/isu`を含む通常APIの正本はs3です。

## Reproducibility hashes

| Artifact | SHA-256 |
|---|---|
| `8dc3772:webapp/go/main.go` | `4ee40fa4c9f6ea524cb1a9e2ad7360fe5eb4f3cfa94c3c0f9a270605ef32f173` |
| R4 `config/nginx/isucondition.conf` | `e5fa33f05247308ec49191dbbe28e0087e64360626fd1e4028f97d57db1c6273` |
| HEAD `config/nginx/isucondition.conf` | `5ffd14de688620a8c0251e07cecda38e319569d5f7752511f869ec1b29bdae36` |
| `config/nginx/nginx.conf` | `10cb2bce3bb674077c960f0a3910972d7184b97cec111669e40dab64514328b9` |
| `config/mariadb/99-isucon.cnf` | `b7462f1f41615fa7a733871d2aed3969973708e02497d0f5526e8e4e10ea31af` |
| `webapp/sql/0_Schema.sql` | `a0d264994e4a28655cdba49eb4fa63d2cdc58ebfee900b9a3b701df199f5f824` |
| `webapp/sql/init.sh` | `e4a71dc633e72dbf327fdfbecf886cc15fe22ecab661b629a8cb37b132452c19` |

## Final-mode evidence

- guard source: `user: extended through 2026-07-22; Portal docs use 22:00 retest boundary`
- freeze: `2026-07-22T21:00:00+09:00`
- benchmark close: `2026-07-22T21:55:00+09:00`
- no access: `2026-07-22T22:00:00+09:00`
- `bin/isuctl final`: 21:00 JSTに成功。全3台をfinal modeへ切り替え、再起動後の`final-check.sh`まで完了。
- post-reboot doctor: 21:01 JSTに成功。s1 Nginx、s2 MariaDB、s2/s3 Appがactive。trigger watcherは全台停止。
- local watcher: 停止済み。
- reboot後official final: Portal実行後にbenchmark ID、score、validity、deduction、timeoutをここへ追記する。

## Score funnel

R4の6,379,010点は次の合計です。

| Funnel | Points | Share |
|---|---:|---:|
| start | 1,000 | 0.02% |
| completed graph | 5,758,750 | 90.28% |
| today graph | 348,816 | 5.47% |
| condition read | 270,444 | 4.24% |

650万点との差は120,990点です。650万点を超える最短の目安は`GraphGood`を807回増やすことです。`GraphWorst`やPOST総数を単独で追わず、完成グラフの周回数とGood密度への変換を優先します。

## Remaining final checks

1. Portalでs1を対象に公式benchmarkを1回だけ開始する。
2. `PASSED / deduction 0`、benchmark ID、score、timeoutを記録する。
3. このmanifestと台帳をcommitし、同branchへpushする。
4. 22:00 JST以降はSSH、doctor、sync、Portalを含む全アクセスを行わない。
