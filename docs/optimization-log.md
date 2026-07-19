# PISCON 2026 AI Agent optimization log

この文書は、公式ベンチマークと計測結果を根拠にした改善履歴です。raw access log、
slow query log、profile、inventory、鍵、Cookie、認証情報は記録しません。

## Session

- 開始: 2026-07-19 13:42:07 JST
- 制御リポジトリ開始commit: `d8cf761de2491a6b0a6a03d3f6d10e225d63688a`
- アプリ開始commit: `9953cf2708e3b9fa213ac78f7af21e2dc93b597e`
- 作業branch: `agent/piscon-10h-20260719-1342`
- ベンチマーク時間: 60秒
- 採用条件: portal resultが`PASSED`で、説明できない互換性エラーや減点がないこと

## Official constraints used

- [PISCON 2026 portal documentation](https://piscon.trap.jp/docs)
- [ISUCON11 qualifier manual](https://github.com/isucon/isucon11-qualify/blob/main/docs/manual.md)
- [ISUCONDITION application manual](https://github.com/isucon/isucon11-qualify/blob/main/docs/isucondition.md)
- `POST /initialize`は20秒以内、その後の負荷走行は60秒。
- 通常リクエストは1秒、condition POSTは100msでtimeoutする。
- condition POSTの反映遅延は許容されるが、conditionとgraphは1秒以内に整合する。
- 最終採用には全3台の再起動後にvalidな公式ベンチマークが必要。

## Starting topology

3台とも同一のNginx、Go、MariaDB、JIA mockを起動している。portalで選択した1台だけに
ベンチ負荷が入り、そのサーバー内で`Nginx -> local Go -> local MariaDB`と流れる。
他の2台へ競技trafficを分散する設定はない。

各サーバーは2 vCPU、約3.6 GiB RAM。開始時点では全サービスがactive、measure mode、
active captureなし、アプリの記録commitは3台とも開始commitと一致した。

## Experiments

| ID | 時刻 (JST) | Commit | 仮説・変更 | 根拠 | Benchmark / run | Score / result | 採否・rollback |
|---|---|---|---|---|---|---|---|
| R0 | 13:50 | `9953cf2` | 計測経路のE2E確認。性能改善なし | doctor全項目成功、3台同一run、collector errorなし | synthetic / `20260719T044959.864450Z-s1-981c35` | N/A | 計測基盤を採用。通常の改善中は再実行しない |
| B0 | 13:55 | `9953cf2` | 変更前の公式基準値を取得 | current source、measure mode | `fbd88090-602b-4401-8ae0-56f21f26405f` / `20260719T045525.119132Z-s1-9b11c8` | **1,706 / PASSED**（減点0、timeout 325） | 基準値として採用 |
| E1 | 14:00 | `9c60f92` | 複合indexを追加し、graph/trendの全履歴取得、不要なBLOB転送、request/debug logを除く | B0のslow query上位2件とDB connection待ち | `a96ec8dd-d607-4d43-8a73-d8c72d19b992` / `20260719T050609.671671Z-s1-44cc8d` | **25,394 / PASSED**（減点0、timeout 21） | 採用。B0比14.9倍 |
| E2 | 14:12 | `c57466d` | conditionのlevel絞り込みとLIMITをSQLへ移し、trend N+1を1 queryへ集約。driverのprepare往復を除く | E1のslow query、CPU、access log | `c2668afb-a2f4-48d8-a5c8-c1a161fe6e2f` / `20260719T052721.018505Z-s1-0ecc15` | **10,720 / PASSED**（減点0、timeout 385） | bundleを不採用。trendだけをrollbackして分割検証 |
| E2a | 14:31 | `ef451a6` | E2からtrend集約だけを外し、condition SQL絞り込みとdriver補間を単独評価 | E2の分割実験 | `35aa8306-7338-44e5-8915-e2db5c510288` / `20260719T053400.385716Z-s1-3417a7` | **40,439 / PASSED**（減点0、timeout 33） | 採用。E1比1.59倍 |
| E3 | 14:38 | `5f3a74e` | condition INSERTをpayload単位のmulti-rowへ変更し、MariaDBのcommit durabilityとbuffer poolを競技向けに調整 | E2aのslow queryとOS | `14656924-79f5-4cf4-b918-ea39f2eb302e` / `20260719T054144.836580Z-s1-e50ac7` | **49,531 / PASSED**（減点0、timeout 36） | 採用。E2a比1.22倍 |
| E4 | 14:46 | `a1e3ec3` | E2で単体性能を確認したtrendの相関bulk queryを、write改善後に再導入 | E3でtrend最新取得がSQL時間の54% | `25a26bc9-6d5c-43fd-87c5-ab28b6230d96` / `20260719T054939.323779Z-s1-30286a` | **13,730 / PASSED**（減点0、timeout 404） | 不採用。相関queryをrequest経路から除去 |
| E5 | 14:54 | `4d6b1b3` | ISUごとの最新conditionを専用テーブルへ保持し、trendとisu一覧を単純JOIN化 | E4の相関queryによるDB/connection待ち | `f7f89372-1fdb-45f5-9bd7-015bf9a169f9` / `20260719T055931.812419Z-s1-1b7701` | **15,761 / PASSED**（減点0、timeout 299） | 単独では不採用。構造は保持し、露出したHTTP/CPU飽和を解消して再評価 |
| E6 | 15:05 | `1ad6579` | immutable assetsとSPA入口をNginxから直接配信し、upstream接続をkeepalive化 | E5のstatic約5万reqとGo/Nginx CPU飽和 | `13a415ca-54e5-4e5f-88a2-29daed16d061` / `20260719T060628.660525Z-s1-ad614f` | **21,950 / PASSED**（減点0、timeout 369） | 改善は採用。2 vCPU飽和は残るため3台分離へ進む |
| E7 | 15:14 | pending | s1をNginx+Go、s2をMariaDB、s3をstandbyにし、DB CPUを別ホストへ分離 | E6でGo約70%、MariaDB約55%、Nginx約50%の同居 | pending | pending | 判定待ち。初期化/remote DB/計測不整合またはE6以下ならrollback |

### B0 evidence

- slow queryは約22.5万件、rows examinedは約7,435万行、SQL実行時間合計は約400秒。
- 最新condition取得が1,691回・約209.5秒、trend用の全履歴取得が2,338回・約114.7秒で、
  この2種類だけでSQL実行時間の約81%を占めた。
- fgprofでは`database/sql`のconnection待ちが累積約10,287秒。アプリの最大接続数10本に
  requestが滞留していた。
- 負荷を受けたサーバーはCPU idle約7.4%。MariaDBとGoが主にCPUを使用し、他2台はidleだった。
- access logでは`GET /api/isu`、`GET /api/trend`などが1秒timeoutへ頻繁に到達した。

### E1 expectation

- 最新conditionとtrendのslow query実行時間・rows examinedを90%以上減らす。
- DB connection待ちを減らし、`GET /api/isu`と`GET /api/trend`のtimeoutを明確に減らす。
- 返却JSONと更新処理は変えず、互換性を維持したまま有効スコアを上げる。

### E1 result

- SQL実行時間は約400秒から127秒、rows examinedは約7,435万行から238万行へ減少した。
- DB concurrencyは7.27から2.27、timeoutは325から21へ減少した。
- `GET /api/isu`は平均約785msから60msになった。
- 一方、`GET /api/condition`は3,856回で227万行・339MiBをDBから受け取り、最大20件を
  返すためにGo側で全行を絞っている。全SQL時間の約21%を占める。
- `GET /api/trend`は325回、平均800ms、p95 1秒。最新condition queryを25,243回実行しており、
  全履歴取得は消えたもののN+1の往復が次の壁になった。
- MariaDBのprepare/closeが各約16.9万回発生し、prepareだけで約23.7秒を使った。

### E2 expectation

- condition queryの返却を1回最大20行へ抑え、227万行と339MiBの転送を95%以上減らす。
- trendをrequestあたり多数のqueryから2 queryへ減らし、平均800msを100ms未満へ近づける。
- driverのparameter補間でprepare/closeのwire round-tripを除き、SQL event数とCPU allocationを減らす。

### E2 result and split decision

- condition queryは平均約616行から約21行、合計227万行から3.5万行へ減り、SQL時間も
  約26.6秒から1.4秒へ減った。driverのprepare/close eventも消えた。
- trendは平均800msから126msへ改善し、ベンチは序盤にユーザーを継続的に増やした。
- その増加にwrite処理が耐えられず、condition INSERTは約6.8万回から13.3万回、COMMITは
  6,906回から13,555回へ増加。両者でSQL時間の約75%を占め、timeout 385で評判が悪化した。
- bundle全体はscore低下のため不採用。まずtrend変更だけを外して、残る2変更を分離評価する。

### E2a result

- scoreはE1の25,394から40,439へ向上。condition絞り込みとdriver補間を採用した。
- timeoutは33で、評判悪化は発生しなかった。
- 次のslow query上位はtrendの最新condition取得91,185回・約39.8秒、COMMIT 8,133回・
  約39.5秒、condition INSERT 80,606回・約21.8秒。
- 1 payloadは平均約10 condition。1行ずつINSERTする実装がwrite statementを約10倍にしている。
- MariaDBは`innodb_flush_log_at_trx_commit=1`、buffer pool 128MiB。メモリには余裕があり、
  block device utilは約64%。

### E3 expectation

- INSERT statementを約8万回からpayload数相当の約8千回へ90%減らす。
- `innodb_flush_log_at_trx_commit=2`で各COMMITのfsync待ちを減らし、clean restart時の永続性は維持する。
- buffer poolを1GiBへ広げ、履歴と画像のworking setをOS page cacheだけに依存させない。
- accepted payload、全行validation、transaction境界、HTTP responseは維持する。

### E3 result

- INSERTは80,606回から9,464回、COMMIT時間は約39.5秒から2.0秒、disk utilは約64%から
  9.5%へ減った。multi-rowとMariaDB設定をともに採用した。
- SQL時間合計は128秒から83秒へ減少し、より多いconditionを処理できた。
- 次の支配項はtrendの最新condition取得114,489回・約44.6秒（SQL時間の54%）。
- `GET /api/trend`は1,255回・平均698ms・p95 1秒で、唯一明白に1秒へ張り付くreadのまま。

### E4 expectation

- trendをrequestあたり約92本のlatest queryから2 queryへ減らし、平均を150ms未満へする。
- E2ではこのquery自体は平均約4msだった。write改善後はユーザー増加に耐えて49,531を超える。
- condition read、write、MariaDB設定には触れず、score低下時にtrendだけ戻せる形を保つ。

### E4 result

- scoreはE3の49,531から13,730へ低下し、timeout 404で評判が停止したため不採用。
- `GET /api/trend`の平均は698msから144msへ改善したが、8,368回呼ばれて合計約1,205秒を占めた。
- 相関queryでDB接続待ちが膨らみ、`GET /api/isu`も平均356ms、p95 1秒へ悪化した。
- CPU idleは約10%。query本数だけでなく「1本のqueryがDBで行う仕事」も必ず同時に見る必要がある。

### E5 expectation

- `isu_condition_latest`をinitialize時に1回構築し、accepted conditionと同じtransactionで更新する。
- trendの相関subqueryを単純な主キーJOINへ変え、isu一覧の最新condition N+1も同時に1 queryへする。
- `GET /api/trend`と`GET /api/isu`のp95を200ms未満、timeoutをE3の36以下へ戻し、score 49,531超を狙う。
- 履歴テーブルはgraph/condition API用にそのまま保持し、最新テーブルとの更新原子性も維持する。

### E5 result

- latest JOIN自体は8,213回・合計約4.7秒、平均0.6msで、相関探索は除去できた。
- しかし高速化によりtrendは1,255回から8,221回、accepted conditionは約9千から16,532 payloadへ増えた。
- Goは約70%、MariaDBは約45〜50%、Nginx 2 workerは合計約60% CPUとなり、2 vCPUを使い切った。
- assets 6種とindexだけで約5万requestあり、Goで静的ファイルまで処理する構成が次の明確な壁になった。
- `GET /api/trend`平均138ms、`GET /api/isu`平均101msでも、CPU待ちによりtimeout 299まで増えた。

### E6 expectation

- hash付きassetsをNginxのsendfile/open file cacheで返し、Goへ流れるrequestを約5万件減らす。
- SPAのindexもNginxから配り、localhost upstreamはkeepaliveしてconnect/syscall負荷を減らす。
- APIの意味とDB処理はE5から変えず、CPU idleとGraph/Condition得点が回復するかを分離して見る。

### E6 result

- scoreはE5の15,761から21,950へ39%向上し、Nginx直配信を採用した。
- static routeはALP上位から消え、condition POST平均は約24msから約12msへ改善した。
- その一方、trendは12,391回、accepted payloadは19,098件まで増え、DB queryは約3,190 QPSになった。
- 終盤はGo約60〜75%、MariaDB約50〜58%、Nginx 2 worker合計約50〜60% CPUで2 vCPUを飽和した。
- 同一ホスト内の関数最適化より、未使用2台へCPU負荷を分ける方が次の期待値が大きい。

### E7 expectation

- s1からMariaDBを外し、private network上のs2へ全DB処理を移す。s1の約0.5 coreをAPI/Nginxへ返す。
- initializeのschema/data投入、latest表構築、全APIをremote DBで一貫させる。
- 計測roleもs1=app/nginx、s2=mysql、s3=standbyに合わせ、slow logをs2から回収する。
- network RTT増よりCPU分離効果が上回り、score 21,950超、API timeout減少を期待する。

## Current hypothesis queue

1. `isu_condition(jia_isu_uuid, timestamp)`の複合indexで全表走査を除く。
2. graphを要求日の24時間へSQLで絞り、trendの全履歴取得を最新1件へ縮める。
3. 不要なimage BLOB転送とrequest/debug logを除く。
4. condition POSTをmulti-row化してからdrop率を下げ、得点機会を増やす。
5. 実測後にN+1解消、latest condition構造、hour aggregate、3台構成を判断する。

各変更前に、期待する計測変化とrollback条件をこの表へ追記する。
