# AI Agent向け計測環境

## 何を目指したか

今回の計測環境では「きれいなダッシュボードを作ること」より、Codexが短時間で次の一手を決められることを優先しました。

一回のベンチマークごとに結果を別ディレクトリへ保存し、まず小さな要約を読み、必要になった時だけrawデータへ潜れる形です。出力はJSON、NDJSON、CSV、pprofの標準形式を中心にしました。同じcommitを同じ条件で測り直せることも大切にしています。

PISCONの実機は Ubuntu 20.04、2 vCPU、約3.6 GiB RAM、Go 1.16.5、MariaDB 10.3、Nginx 1.18です。重い監視サーバーを同居させる余裕はあまりありません。そこで、常時動かすものは小さくし、深い調査は目的を決めたベンチだけで行います。

まず全体像だけ掴みましょう。リクエストは、入口の **Nginx**、処理を書く **Goアプリ**、データを保存する **MariaDB** の順に進みます。遅さが見えた時に、どの区間で時間を使ったのかを切り分けるのが計測の目的です。

- `fingerprint`: 数字やUUIDだけが違う同じ形のSQLを、一つにまとめた名前
- `p95`: 100回のうち遅い方から5番目くらいの時間。平均で隠れる遅いrequestを見る値
- `flat` / `cum`: その関数自身が使った時間 / その関数から呼んだ先も含む時間
- `heap` / `allocs`: 現在残っているmemory / これまで確保したmemoryの総量
- `upstream`: この資料ではNginxの後ろにいるGoアプリ
- `run queue`: CPUを使いたいのに順番を待っている仕事の数

## 結論

| 見たい場所 | 採用したもの | 主な出力 | 分かること |
|---|---|---|---|
| DB全体 | Performance Schema | NDJSON | 正規化SQLごとの回数、総時間、平均、読んだ行数、index未使用 |
| DB詳細 | slow query log + pt-query-digest | JSON、text | 実行時間の分布、悪い実例、Rows examined |
| Goアプリ | pprof + route label | protobuf、text、SVG | CPU、割り当て、heap、goroutineを使う関数とAPI経路 |
| Goランタイム | runtime/metrics、DBStats | JSON、JSONL | GC、メモリ、goroutine、DB接続待ち（before/after差分） |
| HTTP | Nginx JSON log + alp | NDJSON、CSV、YAML | APIごとの回数、総時間、p95/p99、エラー |
| HTTP自由分析 | DuckDB | JSON | Nginx時間とupstream時間の差、接続再利用、run横断比較 |
| OS | sar/sadf + pidstat | binary、JSON、text | CPU、メモリ、disk、network、run queue、processの推移 |
| 深掘り | Go trace（別run） | trace、pprof、text | scheduler、network、syscall、同期の待ち時間 |

## DB: Performance Schemaを主役にした理由

これまで通りslow query logだけでも、多くの改善はできます。ただし全queryをファイルへ書くと、ベンチ中にdisk I/Oが増えます。また、runをまたいだログが混ざらないように毎回扱う必要があります。

Performance Schemaの `events_statements_summary_by_digest` は、値だけ違う同型SQLを一つのfingerprintへまとめ、回数・合計時間・平均時間・読んだ行数などをDB内で集計します。ベンチ直前に集計だけをリセットでき、終了後にSQLでNDJSONへ出せます。これはAgentにとってかなり素直な入力です。[MariaDBのdigest table](https://mariadb.com/docs/server/reference/system-tables/performance-schema/performance-schema-tables/performance-schema-events_statements_summary_by_digest-table)、[MySQLのsummary tableとTRUNCATE](https://dev.mysql.com/doc/refman/8.0/en/performance-schema-summary-tables.html)

実機ではPerformance Schemaが初期状態で `OFF` でした。`observability/config/mysql/99-isucon-observability.cnf` で起動時に有効化し、statement digestに必要なinstrumentとconsumerだけを `enable-performance-schema.sql` で有効にしています。

通常runではPerformance Schemaだけを使います。個々の実行のばらつきや実値入りの代表例が必要な時だけ、次のように `slowlog` modeを選びます。

```bash
observability/bin/run-start.sh slow-query-check slowlog
# Portalでベンチマーク
observability/bin/run-finish.sh slow-query-check 12345
```

slow logは終了時に停止し、次のrun開始時にも強制的にOFFへ戻します。中断時は `run-abort.sh` が停止します。`pt-query-digest` の解析はベンチ後なので解析負荷はスコアへ入りませんが、ログを書き出す負荷は入ります。したがって、改善の正しさは通常modeでも測り直します。`pt-query-digest` はfingerprint別の集計に加え `json` / `json-anon` を提供します。[Percona Toolkit公式](https://docs.percona.com/percona-toolkit/pt-query-digest.html)

`slow.private.log` は公開してはいけません。生SQLにはpasswordなどが含まれる場合があります。このリポジトリでは `measurements/` 全体をGit管理から外しています。

### DBの他候補をどう見たか

- `mysqldumpslow`: 追加導入なしで使えるfallbackです。ただしMariaDB 10.3ではJSON出力がなく、percentileなどもpt-query-digestより少ないため主役にはしませんでした。
- `mysqld_exporter + Prometheus`: QPS、buffer pool、lock waitの時系列に強い選択です。3台構成でホスト間の飽和を同じ時間軸に並べたくなったら追加します。fingerprintの正確なrun内集計は直接SQLの方が情報を削らず速いです。[mysqld_exporter公式](https://github.com/prometheus/mysqld_exporter)
- Percona PMM: 長期運用と人間向けQuery Analyticsは強力です。一方、PMM Serverと時系列DBをこの小さな競技機へ置くのは重く、1分のrun境界とも相性がよくありません。今回のコアからは外しました。[PMMのMySQL計測方式](https://docs.percona.com/percona-monitoring-and-management/3/install-pmm/install-pmm-client/connect-database/mysql/mysql.html)

## アプリ: Go標準を主役にした理由

Goには、CPU、heap、割り当て、goroutine、mutex、block、runtime traceを取得する標準機能があります。pprofのraw形式は後から何度でも別の切り口で解析でき、`go tool pprof -top` はAgentがすぐ読めるtextになります。[net/http/pprof公式](https://pkg.go.dev/net/http/pprof)、[runtime/pprof公式](https://pkg.go.dev/runtime/pprof)

診断口は `127.0.0.1:6060` にしかbindしません。pprofには関数名や実装情報が含まれ、外へ公開するものではないためです。Nginxからもproxyしていません。

CPU profileには `method` と `route` のlabelを付けました。`route` は生URLではなく、Echoが持つ `/api/condition/:jia_isu_uuid` のようなテンプレートです。これによりUUIDを記録せず、特定APIだけへ絞れます。label処理はCPU profileを採っている間だけ有効になるため、診断口を置いただけでは全requestに割り当て負荷を加えません。

```bash
/home/isucon/local/go/bin/go tool pprof \
  -top -tagfocus='route=/api/condition/:jia_isu_uuid' \
  webapp/go/isucondition measurements/<run-id>/cpu.pb.gz
```

`/debug/runtime-metrics` はランタイムのGC・memory・goroutineなどをJSONで返します。`/debug/db-stats` は `database/sql.DBStats` を返し、特に `wait_count` と `wait_duration_seconds` からDB接続プール待ちを判別できます。[runtime/metrics公式](https://pkg.go.dev/runtime/metrics)、[DBStats公式](https://pkg.go.dev/database/sql#DBStats)

この二つとallocsは、アプリ起動からの累積値を含みます。そのまま「今回のベンチの量」とは読みません。開始前と終了後を保存し、`runtime-metrics.delta.json`、`db-stats.delta.json`、`allocs.delta.top.txt` に今回の差分を出します。heapだけは終了時点で残っているmemoryです。

mutex/block profileは既定で無効です。常時有効にせず、待ちを疑った診断runだけsystemdの環境変数に `MUTEX_PROFILE_FRACTION` や `BLOCK_PROFILE_RATE` を入れます。CPU profile、mutex/block、traceを同時に重ねると、お互いの計測負荷で結果が読みにくくなるためです。

runtime traceも別runで10秒程度だけ採ります。

```bash
observability/bin/trace.sh trace-check 10
```

`net`、`sync`、`syscall`、`sched` のpprof要約まで自動生成します。今回の実機はGo 1.16.5と古く、新しいGoよりtrace overheadが大きい可能性があるので、score確認runには混ぜません。[Go diagnostics](https://go.dev/doc/diagnostics)

### アプリの他候補をどう見たか

- Pyroscope: 継続profileの比較UI/APIは便利です。数時間にわたる観測や複数ホストでは候補ですが、今回は直接pprofをrunごとに保存する方が構成が小さく、境界も明確です。
- Parca Agent: eBPFでコード変更なしにuser/kernelをprofileできます。ただしroot権限、BTF/kernel相性、backend運用が増えます。標準pprofを埋め込めない言語やkernel/cgoが疑わしい時の第二候補です。[Parca Agent design](https://www.parca.dev/docs/parca-agent-design/)
- OpenTelemetry: 複数serviceをまたぐtraceには強い一方、単一のEcho + MariaDBで最初にhot functionを探す用途には準備が大きすぎます。Profilesは2026年時点でも新しい領域なので、今回は標準pprofを優先しました。[OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/)
- fgprof: CPU時間とI/O待ちを一枚のpprof互換profileにできる便利な追加候補です。goroutine数に応じて採取負荷が増えるため、まず標準traceの `net/sync/syscall/sched` を使います。[fgprof公式](https://github.com/felixge/fgprof)
- BCC/bpftrace/perf: disk latency、run queue、TCP retransmit、native/kernel stackなど、仮説が立った後の深掘りには強力です。Goのgoroutine待ちはOS threadだけを見ても判断しづらいので、アプリの第一手にはしません。

## HTTPとOS

Nginxは `escape=json` を使い、1 requestを1 JSON objectとして出します。全体の `request_time` に加え、upstreamへの接続、header到着、response完了を分けました。[Nginx log module](https://nginx.org/en/docs/http/ngx_http_log_module.html)、[Nginx upstream variables](https://nginx.org/en/docs/http/ngx_http_upstream_module.html)

ざっくり次のように読みます。

- `upstream_connect_time` が大きい: 接続生成、listen backlog、アプリ側の受付を疑う
- `upstream_header_time` が大きい: アプリやDBが最初の応答を返すまでを疑う
- `response_time` が大きい: upstream全体が遅い
- `request_time - response_time` が大きい: Nginx側やclientへの送信を疑う
- `connection_requests` がほぼ1: **clientからNginxまで**の接続が再利用されていない可能性がある（NginxからGoまでの接続ではない）

`alp` はURIの正規化と定型集計が速く、YAML dumpも残せます。[alp公式](https://github.com/tkuchiki/alp) その後段でDuckDBを使い、任意SQLや複数run比較を行います。DuckDB CLIはJSON出力でき、JSONLを直接読めます。[DuckDB CLI](https://duckdb.org/docs/stable/clients/cli/overview)

OSは `sar` のbinaryを一次成果物として残し、終了後に `sadf -j` でJSONへ変換します。binaryがあれば、後からCPUだけ、diskだけ、networkだけと切り直せます。最初は巨大なJSONではなく、各項目の平均だけを集めた `os-summary.txt` を読みます。`pidstat` ではprocess別のCPU、memory、I/O、context switchを1秒間隔で保存します。[sysstat公式](https://github.com/sysstat/sysstat)

Prometheus、node_exporter、Grafanaは最初の1台には入れません。3台構成で「どのホストだけCPUやdiskが詰まったか」を一つの時系列で比較したくなったら、競技機にはnode_exporterだけを置き、Prometheusは別ホストへ置くのがよいでしょう。AgentはGrafana画面よりPrometheus HTTP APIのJSONを直接読む方が速く掘れます。

## 実際の使い方

### 初回だけ

```bash
cd /home/isucon
observability/install.sh
observability/deploy-app.sh
observability/bin/doctor.sh
```

`install.sh` は次を行います。

1. Ubuntu packageの `jq`、`sysstat`、`percona-toolkit` を導入
2. `alp v1.0.21` と `DuckDB v1.5.4` を公式releaseから取得
3. SHA-256を照合して `/usr/local/bin` へ導入
4. MariaDBのPerformance Schemaを有効化して再起動
5. Nginxの構造化ログを有効化してreload

MariaDBの再起動が入るので、初回ベンチの前に一度だけ実行します。Nginxのsite設定は、確認済みのPISCON初期設定と同じ時だけ自動更新します。すでに改善済みの設定なら上書きを拒否するため、差分を見て手で統合します。初期設定は `.before-observability` に退避します。

### 通常の一周

```bash
observability/bin/run-start.sh before-index
# この表示の直後にPortalでベンチマークを開始
observability/bin/run-finish.sh before-index 12345
```

`run-start.sh` は75秒間のCPU profile、sar、pidstat、DBStats採取を始めます。ベンチ開始まで長く待つとprofile区間から外れるので、表示後すぐPortalで開始します。

終了後は、まず次の順で読みます。

1. `meta.json`: commit、mode、score、時刻
2. `mysql-statement-digests.ndjson`: DB時間を最も使ったSQL
3. `alp.csv`: HTTP時間を最も使ったAPI
4. `cpu.top.txt` と `cpu.tags.txt`: CPU hot spotとAPI経路
5. `access-summary.json`: upstream/Nginx/接続の切り分け
6. `db-stats.jsonl`: DB接続待ちの時間変化
7. `os-summary.txt`: CPU、disk、memory、run queueの平均
8. 必要なら `sar.json` と `pidstat.txt`: 1秒ごとの変化とprocess飽和

例えば `mysql-statement-digests.ndjson` の先頭で `total_seconds` が大きく、`rows_examined` が返した行数より桁違いに多ければ、まずそのSQLの検索条件とindexを調べます。`alp.csv` の先頭と `cpu.top.txt` の上位が同じAPIを指せば、次はそのrouteのGoコードへ進みます。このように「一番大きな数字から次のファイルを決める」のが基本です。

改善したらrun IDを変えて同じ手順を繰り返します。scoreだけで判断せず、前runと同じボトルネックが減ったか、新しい場所へ移ったかを確認します。

### Agentへ渡す短い依頼例

```text
measurements/<run-id>/meta.json と、そこから参照される要約ファイルを読んでください。
scoreを守るためrawを全部読む前に、DB総時間、HTTP総時間、CPU flat/cum、OS飽和を比較し、
次の一手を「根拠・期待効果・壊れ得る仕様・次回測る指標」と一緒に一つ提案してください。
変更後は新しいrun IDで同じ計測を行い、前runと比較してください。
```

## 計測の負荷を忘れない

計測は無料ではありません。このツールの自動modeは、基本情報を一度に揃える `standard` と、そこへ全SQLの書き出しを加える `slowlog` の二つです。slow log、trace、mutex/block profile、perf/eBPFを同じrunへ全部載せないでください。何が遅く、何が計測負荷なのか分からなくなります。

最終スコアを確かめる時は、計測を伴わないベンチも別に走らせます。現状のscriptに存在しない「軽量mode」や「score mode」があるようには扱わないでください。

runは同時に一つだけです。同じIDの上書きも拒否します。途中で止める時は次のように後始末します。

```bash
observability/bin/run-abort.sh before-index
```

再現性のため、`meta.json`にcommit、`git.diff`にHEADからのstaged/unstaged差分、`config.sha256`に実行binaryと設定のhashを保存します。untrackedファイルは一覧だけなので、意味のある比較runの前には必要なコードをcommitしてください。

## 成果物と公開範囲

`measurements/<run-id>/` にはprofile、アクセスログ、場合によっては生SQLが入ります。すべてGit管理外です。

講義資料へ載せる時は、次のような要約だけを内容確認してからコピーします。

- `meta.json`
- `mysql-statement-digests.ndjson`（digestで実値は正規化済み）
- `alp.csv`
- `cpu.top.txt` / `cpu.svg`
- `access-summary.json`
- `pt-query-digest.json`（anonymous modeが利用できた場合）

rawの `access.jsonl`、`slow.private.log`、goroutine/profile/traceは、秘密値や実装情報がないか確認するまで公開しません。
