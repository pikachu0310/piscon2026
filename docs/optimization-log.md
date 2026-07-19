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
| E1 | 14:00 | pending | 複合indexを追加し、graph/trendの全履歴取得、不要なBLOB転送、request/debug logを除く | B0のslow query上位2件とDB connection待ち | pending | pending | 判定待ち。invalid、減点、query plan悪化、またはスコア非改善なら変更を分割してrollback |

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

## Current hypothesis queue

1. `isu_condition(jia_isu_uuid, timestamp)`の複合indexで全表走査を除く。
2. graphを要求日の24時間へSQLで絞り、trendの全履歴取得を最新1件へ縮める。
3. 不要なimage BLOB転送とrequest/debug logを除く。
4. condition POSTをmulti-row化してからdrop率を下げ、得点機会を増やす。
5. 実測後にN+1解消、latest condition構造、hour aggregate、3台構成を判断する。

各変更前に、期待する計測変化とrollback条件をこの表へ追記する。
