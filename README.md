# PISCON 2026

PISCON（ISUCON11予選問題）のGo実装と、AI Agentがボトルネックを調べるための計測環境です。

- アプリ: [`webapp/go`](webapp/go)
- DB初期化: [`webapp/sql`](webapp/sql)
- 計測ツール一式: [`observability`](observability)
- 選定理由と使い方: [`docs/observability.md`](docs/observability.md)

計測結果は `measurements/<run-id>/` にまとまります。このディレクトリはGit管理しません。rawログにはURLやSQLの実値が含まれる可能性があるためです。

実機での最短手順は次の通りです。

```bash
cd /home/isucon
observability/install.sh
observability/deploy-app.sh
observability/bin/doctor.sh

observability/bin/run-start.sh baseline
# すぐにPortalからベンチマークを開始する
observability/bin/run-finish.sh baseline 12345
```

途中でベンチや計測を取りやめた時は `observability/bin/run-abort.sh baseline` で後始末できます。

最初に読むべき結果は `meta.json`、`mysql-statement-digests.ndjson`、`alp.csv`、`cpu.top.txt`、`access-summary.json`、`os-summary.txt` です。
