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

- run ID: `loop3-no-echo-log-retry`
- commit: `e41bd4c`
- score: **20,486（PASSED）**
- 前回比: **+462（+2.3%）**
- 計測error: 1（Portalの待機後に120秒samplerが終了しきらず、DBStats samplerだけを終了。主要なraw計測は取得済み）

Portalが一度 `WAITING` になったため最初のrunは中断し、同じcommitで計測を開始し直しました。CPUの `syscall.Syscall` は15.73%から13.47%へ下がりましたが、scoreへの影響は小さめでした。

### 次に見るもの

`GET /api/trend` が平均946ms、合計212秒でHTTP側の最大の外れ値です。成功は224回中24回だけで、多くがclient timeoutになっています。

## 4. trendのN+1 queryをまとめる

### 見た計測結果

`getTrend` は次の順でqueryを実行していました。

1. 性格の一覧
2. 性格ごとのISU一覧
3. ISUごとのcondition全履歴
4. Go側では各ISUの先頭1件だけを使用

最新1件しか使わないのに全履歴を転送し、ISU数に比例してquery回数も増えるN+1構造です。CPU profileでもMySQL row decodeと `database/sql.convertAssignRows` が上位へ上がっています。

### 修正

性格一覧のqueryと、全ISUの最新conditionだけを取るqueryの合計2回にまとめます。レスポンスの分類とtimestamp降順はGo側で従来通り維持します。

### スコア

- run ID: `loop4-trend-query`
- commit: `c15128f`
- score: **10,634（PASSED）**
- 前回比: **-9,852（-48.1%）— 回帰**
- 計測error: 0

queryをまとめたことでtrend平均は946msから136msへ改善しました。しかし成功できるrequestが増え、trend自体が224回から6,227回へ急増しました。新queryは6,149回呼ばれ、合計39.74秒・走査266万行です。DB接続待ちも731秒から4,120秒へ悪化しました。

「1回の処理を速くした」だけで、同じ計算を何千回も繰り返す構造を残したことが回帰の原因です。このcommitは途中経過として残し、次の変更でqueryを通常経路から外します。

## 5. 最新trend状態をGoメモリへ保持する

### 見た計測結果

- trend query: 6,149回、合計39.74秒
- 同じrun中のISUは最大でも数百台で、返すのは各ISUの最新conditionだけ
- condition POSTの処理時点で、最新状態を更新するための全情報をすでに持っている

### 修正

- `/initialize` 後にDBから最新状態を一度読み込む
- ISU登録時にIDと性格を追加
- conditionのcommit成功後に、timestampが新しければメモリ上の最新状態を更新
- `/api/trend` はmutexで保護したメモリ状態を読み、DB queryを実行しない

POSTへの応答を返す前にcacheを更新するため、そのPOSTを送ったclientから見た整合性を維持します。これは単一アプリサーバー構成を前提にした改善です。

### スコア

初回試行は **0点（FAILED）** でした。`/initialize` 内のcache構築queryで、予約語の `character` をbacktickで囲まずMariaDBのsyntax errorになったためです。run `loop5-trend-memory` は負荷計測として成立しないので中断しました。

`character` を `` `character` `` に直し、同じ仮説を再計測します。

再計測ではtrendからDB queryが消え、`GET /api/trend` は11,878回すべて成功し、平均20msになりました。一方でscoreは **16,133（PASSED）** までしか戻りませんでした。

- run ID: `loop5-trend-memory-retry`
- commit: `9b9c04f`
- score: **16,133（PASSED）**
- 前回の正常run比: **-4,353（-21.2%）**
- 計測error: 0

成功するrequestが増えた結果、Goのjournalに `accept4: too many open files` が連続して現れました。その時間帯には静的ファイルの404やtimeoutが発生し、48件の減点がありました。trend cacheの狙いは達成できていますが、今まで負荷の陰に隠れていた同時接続の上限が次のボトルネックです。

## 6. 実プロセスのファイルディスクリプタ上限を上げる

### 見た計測結果

- systemdの `LimitNOFILE` 表示: 524,288
- 実際のGoプロセスの `/proc/<pid>/limits`: soft 1,024 / hard 524,288
- 負荷終了後のopen FD: 10
- system全体のopen file: 1,472、上限1,048,576

OS全体の枯渇や恒常的なFD leakではなく、負荷中だけプロセスのsoft limit 1,024へ到達したと判断できます。設定ファイルの値だけでなく、実際に動くprocessの `/proc` を確認したことで差に気づけました。

### 修正

systemdのdrop-inでsoft/hardの両方を65,536に明示し、applicationのdeploy時にdrop-inの配置と `daemon-reload` も行います。再起動後は `/proc/<pid>/limits` を再確認してから計測します。

### スコア

- run ID: `loop6-fd-limit-retry`
- commit: `971ff46`
- score: **15,285（PASSED）**
- 前回比: **-848（-5.3%）**
- 計測error: 0

再起動後の実プロセスはsoft/hardとも65,536になり、負荷中の `too many open files` は0件になりました。1回目はPortalの待機が長く17,935点でしたが、pprofが負荷全体を覆わなかったため、判断にはすぐ開始できた再計測を使います。

FD枯渇は直りましたがscoreは回復しませんでした。これは「上限を上げれば速くなる」のではなく、エラーで早く失敗していたrequestが最後まで処理され、次のボトルネックへ負荷が届くようになったためです。

### 次に見るもの

- DB接続待ち: 37,094回、合計7,346秒
- DB pool: 最大10接続
- CPU idle平均: 45.74%
- disk busy平均: 39.33%
- `COMMIT`: 16,093回、合計71.38秒

CPUもdiskも余力があるのに、GoのDB pool上限10でrequestが長時間待っています。

## 7. DB connection poolを広げる

### 見た計測結果

`/debug/db-stats` は、Goの `database/sql` 内部でDB接続を待った時間を直接示します。37,094回・7,346秒という値は、遅いSQLを探す前に「SQLを実行する接続を借りられない」時間が支配的だという意味です。また、60秒で1,081接続がidle poolから閉じられており、再接続も多発しています。

### 修正

最大open connectionと最大idle connectionを10から100へ増やします。MariaDBの上限内に収めつつ、まずアプリ内の待ち行列を減らし、CPU・diskに残っている余力を使えるか確認します。

### スコア

- run ID: `loop7-db-pool`
- commit: `38cdf8d`
- score: **9,771（PASSED）— 回帰**
- 前回比: **-5,514（-36.1%）**
- 計測error: 0

主な変化:

- DB接続待ち: 15,923回、合計14,703秒
- `COMMIT`: 平均4.44ms → 8.31ms、合計71.38秒 → 136.08秒
- CPU idle: 45.74% → 32.83%
- disk busy: 39.33% → 46.20%
- condition POST平均: 14ms → 34ms

接続を100本にすると、Go poolで待つrequest数は減りました。しかしMariaDBへ同時に流しすぎたため1 transactionが遅くなり、待ち時間の合計は約2倍です。ボトルネックを手前から奥へ移しただけで、処理能力は上がっていません。この値は採用せず、次は20接続に絞ります。

## 8. DB connection poolを20に調整する

### 見た計測結果

10接続ではDB接続待ちが長い一方、100接続ではCOMMIT自体が約2倍遅くなりました。接続数は多ければ多いほど良い設定ではなく、このサーバーでMariaDBが効率よく処理できる並列度を計測で探す必要があります。

### 修正

最大open/idle connectionを20へ下げます。10よりは待ち行列を短くしつつ、100接続で起きたDB内部の競合を避ける狙いです。

### スコア

- run ID: `loop8-db-pool-20`
- commit: `474b622`
- score: **20,139（PASSED）**
- 前回比: **+10,368（約2.1倍）**
- これまでの最高値20,486との差: **-347（-1.7%）**
- 計測error: 0

主な変化:

- DB接続待ち: 32,023回、合計4,991秒
- `COMMIT`: 平均4.97ms、合計81.20秒
- condition POST平均: 17ms
- CPU idle: 63.22%

Portal待ちが約40秒あり、pprofとOS samplerは負荷の前半約55秒を含みました。access logとPerformance Schema digestはベンチ全体を取得しています。

20接続は100接続の回帰を解消し、10接続より接続待ち合計も短くできました。ただし最高scoreとの差は小さく、1回だけで優劣を断定できない範囲です。ここでは20を暫定採用し、次の明確な支配項であるCOMMITを調べます。

## 9. COMMITごとの同期書き込みを緩和する

### 見た計測結果

- condition POSTのうち、保存対象になった約16,000件がそれぞれtransactionをCOMMIT
- `COMMIT`: 16,345回、合計81.20秒、平均4.97ms
- SQL本文の上位ではなく、COMMITがDB時間の大半

condition POSTは採用する10%だけtransactionを開始しているため、不要なtransactionを消す余地はありません。1件ごとにredo logをdiskへ同期する耐久性設定のコストが目立っています。

### 修正

MariaDBの `innodb_flush_log_at_trx_commit` を1から2へ変更します。COMMIT時にはOS cacheへ書き、diskへのflushはおおむね1秒ごとになります。通常運転中の整合性は維持しますが、OSやinstanceが突然停止すると直前約1秒の更新を失う可能性があります。競技中にこのtrade-offを許容する場合だけ使う設定です。
