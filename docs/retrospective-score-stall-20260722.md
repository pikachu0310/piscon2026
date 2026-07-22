# PISCON 2026: なぜ長時間動かしても16万点で止まったのか

## 結論

主因はモデル能力より、仕事の切り方と与えた情報境界にある。

今回のAgentは既存構造の内部をかなり高速化した。しかし、公式benchmarkを細かい変更ごとに回し、
大量の記録と再測定を行ったため、得点生成系を丸ごと置き換える長い実装区間がなかった。さらに、
以前の300万点実装、公式benchmark source、公式解説、他teamの解法を自分で禁止していたため、
既に分かっていた構造を再発見するところから始めてしまった。

次のrunでは「現在の実装を少しずつ速くする」のではなく、benchmarkがユーザーと得点を増やす流れを
先に完全に理解し、App、state ownership、DB、3台構成を一つの設計として作り直す必要がある。

## 計測で確認できる差

### 今回のcontest-only run

- B0は134,310点、最高のB13は162,980点。scalar scoreは約21.3%伸びた。
- B0からB33まで約4時間で34回の公式runを行った。Portal errorのB34/B35も別にある。
- `e95ccd1`から`9af19c5`までは4.29時間で47 commit、平均commit間隔5.6分だった。
- 46個のcommit間隔のうち41個が10分未満で、30分以上の間隔は一度もなかった。
- contest-only台帳は679行、frontier台帳は342行まで増えた。
- 後半B19〜B33はdecoder、buffer pool、private batch encode/decode、snapshot、sender数などを
  改善し、単位CPU costとaccepted workは伸ばしたが、score championはB13のままだった。

この密度では、1回60秒のbenchmarkそのものより、deploy、Portal、log同期、解析、台帳、commit、
次の小変更という周辺作業が支配的になる。Agentは忙しく動いたが、30〜120分かかる再設計へ入れなかった。

### 以前の300万点run

- 初期1,284点から最終3,072,854点まで伸びた。
- 初期commitから再現資料commitまでは15.89時間で、主要commitは5個だった。
- 初期との差分は約4,527行追加で、`fast_handlers.go` 1,311行、`cluster.go` 488行、
  `raw_condition_server.go` 225行を新設した。
- condition履歴をDB hot pathから外しただけでなく、全APIを専用in-memory handlerへ置換した。
- 2 workerがUUID shardの正本を持ち、condition/graphをownerで処理した。
- workerからcoordinatorへ最新conditionだけを非同期pushし、trendの全worker問い合わせを消した。
- JIA activateより前の仮登録と、activate成功後の公開を分けた。

以前も62回benchmarkしており、単に「測らなかった」わけではない。違いは、複数の関連変更を一つの
architecture milestoneとして作り、benchmarkを設計の確認に使った点にある。

なお3,072,854点は再構築した外部benchmarkで、PISCON公式Portalの162,980点とは測定系が違う。
同じ外部構成でも1,394,902、2,868,779、1,910,203、3,072,854と大きく揺れているため、
「以前のAgentが厳密に30倍強かった」とは断定できない。一方、現在のPISCONで650万点が出ているなら、
現在の公式系列にも大きな未利用余地があることは確かである。

## 原因の優先順位

### 1. 公式benchmarkを評価器ではなく開発ループとして使いすぎた

元promptは30回以上、平均4回/時、5実験ごとのchampion再測定、1時間ごとの総括を要求した。
実際はさらに高密度になった。これは仮説の検証には強いが、構造を書き直す時間を奪う。

次はlocal test、microbenchmark、公式benchmark sourceから作るscenario testで変更をまとめる。
公式Portalは「このbundleでscore funnelが一段進んだか」を確認するときだけ使う。

### 2. 使用禁止にした情報が、今回もっとも価値の高い情報だった

前promptは次を禁止していた。

- `/home/pikachu0310/github/isucon11-ai-agent-2026`
- goal開始前のGit historyと`docs/optimization-log.md`
- 公式benchmark/JIA内部source
- 公式解説、他teamのwrite-upとcode

その結果、過去に失敗済みのgzip、request buffering、細かなHTTP/GC調整を繰り返した。E35、E52、
contest-onlyのB2/B3は同じstatic compression系統であり、bufferingも複数回往復している。
高得点構造を知っていながら使わない、という実験としては意味があったが、順位を取る条件ではない。

### 3. scoreの因果を「総成功request数」で粗く見すぎた

公式source上、直接加点される中心は新しいconditionのGETとgraphである。condition POSTは重要な
材料だが、それ自体を大量に202にしても直接同じ重みで加点されない。trendの変化はuser増加を生み、
登録成功はposterと将来のread/graph loopを増やす。

したがって見るべきfunnelは次である。

```text
登録user/ISU
  -> poster開始
  -> ISU・時間帯ごとの有効condition密度
  -> 更新されたtrend
  -> user増加
  -> condition read / graph完走
  -> score breakdown
```

今回の`tracked successes`は、得点への距離が異なるrequestを合計した。private transportを安くして
condition 202を増やすことは正しいfrontierになり得るが、read/graph/user増加へ変換できないまま
15 run続けたのは長すぎた。

### 4. partial distributionで止まり、単一authoritative stateを残した

現在のcontest-only構成はs1が全外部trafficを受け、s2 edgeがconditionをdecode/batchし、最後は
s3の唯一の`ConditionState`へforwardする。s2を使えてはいるが、同じconditionに外部decode、
private encode、HTTP、private decode、s3 appendという二重処理があり、s3が直列化点として残る。

以前の高得点構成は2 workerがUUID shardの正本を持ち、condition GETとgraphもownerへ送った。
coordinatorへ運ぶのはtrend用の最新状態だけだった。今回必要なのはprivate hopのallocation削減より、
hopと単一正本そのものをなくす設計である。

### 5. full in-memory applicationへの移行が途中だった

現在も認証時のuser確認、登録transaction、icon、一部fallback readなどにDB経路が残る。Echo router、
一般JSON、DBを個別に少しずつ削っている。一方、以前の構成はinitializeで完全な世代を読み込み、
全公開APIをfast handlerへ切り替え、benchmark中のDB利用を最小にした。

ただし、PISCONの最終確認ではbenchmark中に書かれたdataの再起動後取得が必要である。次の再構築は
memory-onlyで終わらせず、ownerごとのappend log、非同期batch DB、snapshotなど、score hot pathと
再起動永続性を両立する必要がある。

### 6. 大規模案をintegration failureと1〜2回のscoreで早く畳んだ

E47では以前の3-node architectureを移植したが、port 80漏れ、chunked body非互換、seed file rollback
事故が起きた。修正後は164,644点を一度出したが、再測定158,691点と複雑さ、そして当時の情報境界を
理由に全撤回した。構造仮説そのものより移植品質と環境差が主要因だったのに、完成度を上げる前に
評価を終えた可能性が高い。

大規模変更は小変更より最初のfailure率が高い。次はcompatibility suiteとshadow comparisonを先に作り、
配備事故とarchitectureの性能を分ける。FAILなら即撤回せず、仕様差を直してvalidを取る。

### 7. promptの要求が多く、優先順位が競合した

前promptは、3種類の構造案、30 run、5軸評価、定期再測定、毎時総括、manifest、詳細台帳、finalを
すべて必須にした。安全で説明可能な研究には向くが、「12時間で1位」に対しては儀式が多い。

次のpromptでは目的、情報解禁、benchmark cadence、構造優先、finalの5点だけを強くする。
台帳は1 run 1行とarchitecture decisionだけに制限する。

### 8. 実際には新しい10時間を完走していない

contest-only commit列は4.29時間で終わっている。別の10時間continuationにも長いplatform中断がある。
運営時刻の解釈が正しかったかとは別に、成果を「10時間の同条件run」と比較することはできない。
次は開始時にPortalの現行deadlineを読み直し、古い`contest-window.tsv`を現在の大会時刻へ更新する。

## 次のrunで変えること

1. 以前の高得点repo、全Git history、公式benchmark source、公式解説、上位team codeを最初から使う。
2. 最初の60〜90分でscore funnelとtarget architectureを決め、その後は最低30分の実装区間を守る。
3. 小変更を3〜8個の因果的bundleへまとめ、公式benchmarkは原則1〜2回/時にする。
4. condition POST件数だけでなく、user増加、ISU数、hour bucket密度、scored read、graph tierを追う。
5. s1 ingress、single authoritative App、DB hot pathを残す理由を一つずつ立証する。立証できなければ除く。
6. 以前のfull-memory/shard/async-push/two-phase registrationを出発点にし、PISCON差分だけを直す。
7. score championを常に復元可能にしつつ、最後の1時間まで高期待値の構造変更を続ける。

## 根拠

- 今回の実験: [`optimization-log-contest-only.md`](optimization-log-contest-only.md)
- score/capacityの分離: [`score-frontiers.md`](score-frontiers.md)
- 以前の300万点run: `/home/pikachu0310/github/isucon11-ai-agent-2026/records/SCORES.md`
- 公式benchmark source: `/home/pikachu0310/github/isucon11-qualify-official/bench/`
- [ISUCON11予選問題の公式解説](https://isucon.net/archives/56044867.html)
- [予選1位shallowverseの記録](https://y1r.org/posts/20210831-isucon11-qualify/)
