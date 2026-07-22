# PISCON 12時間・1位獲得優先プロンプト

## 目的

PISCON公式Portalで`PASSED / deduction 0`を守り、現在約650万点の1位を超える。
12時間の能動作業で、現在のApp・DB・3台構成を必要なら全面的に作り直す。
小さな改善数、説明量、変更量ではなく、最終的な有効scoreを最大化する。

## 情報と変更の許可

以前の情報制限はすべて撤回する。現在sourceと実機に加え、次を積極的に使う。

- `/home/pikachu0310/github/isucon11-ai-agent-2026`の300万点構成と全履歴
- 現repoの全branch、Git history、過去log、未採用案
- `/home/pikachu0310/github/isucon11-qualify-official`のmanual、benchmark、JIA source
- 公式解説、上位teamのwrite-up・公開repo、一般的な技術資料、Web検索

実装の流用、全面置換、独自protocol、in-memory化、DB schema変更、3台のrole変更を許可する。
ただし競技規約、API互換性、再起動後の永続性を守り、benchmark/JIA自体は改変しない。

## 最初の90分

1. Portalの現在の終了時刻を確認し、古いdeadline設定を現在のPISCON日程へ更新する。
2. 現在のscore championをcommit、binary/config hash、topology付きで退避する。
3. 公式benchmark sourceを読み、次のscore funnelを数値で作る。

```text
登録user/ISU -> poster開始 -> 有効condition密度 -> trend更新
-> user増加 -> condition read / graph完走 -> score breakdown
```

4. read-only subagentを3体並列起動する。
   - A: benchmark/JIA/公式解説/上位解法から、650万点へ必要な得点機序を特定する。
   - B: 現在実装と以前の300万点実装をdiffし、移植すべき構造とPISCON固有差分を出す。
   - C: 現在の3台、network、resource、永続化、deploy/finalを調べ、全面移行手順を作る。
5. baselineは1回だけ取得し、90分以内に一つのtarget architectureを決めて実装を始める。

main agentだけが編集、deploy、Portal benchmarkを行う。subagentには調査、最新run分析、test reviewを
継続して任せ、main contextへraw logを大量に持ち込ませない。

## 改善方針

最優先は、現在のsingle-authoritative Appを少し速くすることではなく、得点経路全体を短くすること。
次を必須の構造仮説として検証し、棄却するなら実測理由を残す。

- 全公開APIをinitialize世代のin-memory stateから返し、DBをscore hot pathから外す。
- conditionの正本をUUIDで複数nodeへshardし、不要なedge→authoritative再転送を消す。
- 許可されるならJIAの`target_base_url`で外部condition ingress自体を3台へ分散する。
- condition GET/graphはownerで処理し、trend用には最新状態または集計だけを非同期共有する。
- registrationはactivate開始、仮登録、condition受理、公開、失敗rollbackを二段階化する。
- graph/trend/listはrequestごとに走査せず、incremental aggregateとimmutable snapshotを返す。
- benchmark中のwriteはmemoryとappend/batch永続化を両立し、全台reboot後も取得可能にする。

以前の高得点実装をそのまま信じず、まず移植してPISCON差分を直す。E47のport 80、chunked body、
seed rollback、FQDN制約はarchitecture失敗ではなく既知のintegration課題として先に潰す。

いずれかのrequestが毎回DB、全件走査、全node同期、JSON再encode、single global lockを通る限り、
allocationやparameterの微調整を優先しない。

## benchmark loop

- 小変更ごとに公式benchmarkを回さない。関連する3〜8変更を一つのscore機序を持つbundleにする。
- local test、race、compatibility test、microbenchmark、shadow response比較でbundleを育てる。
- 公式benchmarkはarchitecture milestone、bottleneck移動、20%以上の改善見込み、final候補の確認に使う。
  目安は1〜2回/時であり、回数を稼がない。
- 大規模変更の最初のFAILは即rollbackせず、integration問題を直して少なくとも1回validを取る。
- 同一構成の再測定はvariance判断またはfinal候補に限り、原則2回までにする。
- 各runはscoreだけでなくfunnelの各段、score breakdown、node別CPU/network/queueで判断する。
- score低下でも上流funnelと単位costが改善し下流飽和を説明できるなら、最大2つの次bundleまで続ける。
  ただし総成功request数だけを根拠にせず、scored read/graph/user増加への変換を必ず確認する。
- 2回の公式runまたは60分でscore funnelが一段も進まなければ、同系統のmicro tuningを止めて構造を変える。
- 記録は1 run 1行とarchitecture decisionだけにし、作業中に長い資料を書かない。

## 時間配分

- 0〜1.5h: score source、既存高得点実装、現在構成の差分を理解しtarget architectureを確定。
- 1.5〜5h: full-memory・state ownership・3台経路を一つの大きなbundleとして完成させる。
- 5〜9h: registration、trend/user増加、condition密度、read/graphの次の制約を順番に外す。
- 9〜11h: 650万点との差をscore breakdownから詰め、高risk/high-reward案も試す。
- 11〜12h: score championを復元し、計測停止、全台reboot、smoke、公式finalを行う。

各時間帯は目安であり、証拠に基づき変えてよい。ただし最低30分の連続実装区間を確保し、
Portal操作・台帳・commitで実装時間を細切れにしない。benchmarkの1分中は次bundleを進める。

## 完了条件

12時間を経過しただけでは完了ではない。次をすべて満たす。

- session最高と、全台reboot後finalの両方が`PASSED / deduction 0`
- 最終構成、DB/schema、service、3台roleがGitとmanifestから再現可能
- benchmark中に生成された必要dataがreboot後も取得可能
- 現在のscore championを明確に更新し、650万点未達なら最大の残存制約を数値で説明
- 変更、失敗、rollback、benchmark ID、score funnelの推移を最小限の台帳へ保存してpush

ユーザー入力が本当に必要な権限問題以外では止まらない。現在の小さなbottleneckへ固執せず、
1位に必要ならApp、DB、protocol、3台構成を何度でも大きく作り直す。

## `/goal`へ送る文

```text
/goal /home/pikachu0310/github/piscon2026-observe/prompts/continue-12h-first-place-unrestricted-ja.mdを最初から最後まで読み、この契約に従ってPISCON公式Portalのvalid・deduction 0スコアを12時間で最大化せよ。以前の情報制限は撤回済みで、過去の高得点repo、公式benchmark source、公式解説、上位解法を積極的に使え。小変更ごとにbenchmarkせず、App・DB・state ownership・3台構成を必要なら全面再構築し、650万点超とreboot後finalを目指せ。
```
