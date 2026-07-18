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

計測後に追記します。
