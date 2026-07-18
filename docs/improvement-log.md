# 改善ログ

このページでは、AI Agentが実際に行った改善を、毎回同じ順番で記録します。

1. どの計測結果を見たか
2. 何を直したか
3. Portalのスコアはどう変わったか
4. 次に何を調べるか

計測中のprofileやアクセスログには秘密値が含まれる可能性があるため、rawデータはサーバーの `/home/isucon/measurements/` にだけ保存します。ここには判断に使った数値と結論を残します。

## 0. 計測環境導入直後

- run ID: `baseline-observability`
- commit: `f4b8600`
- score: **1,717（PASSED）**
- 計測error: 0

見えたこと:

- DB接続待ちは14,490回、合計12,276秒。1回あたり約0.85秒
- `COMMIT` は7,506回、合計37.44秒
- `POST /api/condition/:uuid` は70,647回、合計624秒
- CPUのflat上位は `syscall.Syscall` 22.62%、`runtime.epollwait` 10.39%
- CPUのroute labelではcondition POSTが約67%

## 1. 捨てるrequestの警告ログを止める

### 見た計測結果

CPU profileでOSへの書き込みを含む `syscall.Syscall` が22.62%を占めていました。コードを確認すると、90%のcondition POSTを意図的に捨てるたび、Echoのloggerへ警告を1行書いていました。アクセスログ上ではcondition POSTが70,647回あるため、数万行の不要なjournal出力になります。

### 修正

捨てる確率やHTTP statusは変えず、`drop post isu condition request` の警告ログだけを削除しました。これは仕様上の応答を変えず、ログI/Oの影響だけを比べるための小さな変更です。

### スコア

- run ID: `loop1-no-drop-log`
- commit: `b46c55e`
- score: **2,278（PASSED）**
- 前回比: **+561（+32.7%）**
- 計測error: 0

ログを外した後も `syscall.Syscall` は21.96%でした。これはHTTPやDB通信にも使われるため、警告ログだけが原因ではありません。一方、同程度のcondition POSTを受けながらスコアは大きく上がり、DB接続待ち合計も12,276秒から9,809秒へ減りました。

### 次に見るもの

遅いGET APIとSQLの検索方法を確認します。

## 2. condition検索用indexを追加する

### 見た計測結果

- `GET /api/isu`: 平均732ms、合計275秒
- `GET /api/condition/:uuid`: 平均240ms、合計88秒
- `GET /api/trend`: 平均830ms、合計37秒
- `GET /api/isu/:uuid/graph`: 平均177ms、合計29秒
- DB接続待ち: 14,770回、合計9,809秒

これらの実装は、`isu_condition` を `jia_isu_uuid` で絞り、`timestamp` 順に読むqueryを繰り返します。しかしschemaには主キーしかありません。実機の `EXPLAIN` でもcondition検索は `type=ALL`、約70,957行の全走査とfilesortになっていました。`isu` のユーザー別一覧も約72行を全走査していました。

### 修正

- `isu_condition (jia_isu_uuid, timestamp)` の複合indexを追加
- `isu (jia_user_id)` のindexを追加

queryやレスポンスは変えず、必要な行へ辿る方法だけを変えます。

### スコア

- run ID: `loop2-indexes`
- commit: `99073b0`
- score: **20,024（PASSED）**
- 前回比: **+17,746（約8.8倍）**
- 初回比: **約11.7倍**
- 計測error: 0

主な変化:

- `GET /api/isu`: 平均732ms → 51ms
- `GET /api/condition/:uuid`: 平均240ms → 56ms
- `GET /api/isu/:uuid/graph`: 平均177ms → 44ms
- DB接続待ち合計: 9,809秒 → 725秒
- CPU idle平均: 27.17% → 62.94%

小さなschema変更ですが、遅いqueryだけでなくDB接続を長時間占有する原因も消えたため、待っていた他のrequestまで一緒に速くなりました。

### 次に見るもの

CPU profileでは `syscall.Syscall` が15.73%で、まだ最大です。Goの起動コードを見るとEchoのrequest loggerが全requestを標準出力へ書き、別途Nginxも同じrequestをJSON記録しています。

## 3. 重複するEcho request logを止める

### 見た計測結果

- `syscall.Syscall`: CPU flat 15.73%
- Nginx access log: 約9万requestをすでに記録
- Go側でも `middleware.Logger()` が同じ単位で標準出力へ記録

condition POSTだけでも74,655回あり、競技中に同じアクセスログを二重に書く必要はありません。

### 修正

Echoの `middleware.Logger()` を外します。panicから復帰する `middleware.Recover()` と、分析用のNginx JSON access logは残します。

### スコア

計測後に追記します。
