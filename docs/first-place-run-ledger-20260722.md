# PISCON 1位狙い再開ログ（2026-07-22）

このページは、公式Portalで確認した得点と、その時点で実際に配備されていたGit commit・binary・構成を対応づける台帳です。スコアだけを見て小変更を往復せず、得点源が変わる単位でまとめて計測します。

## R0: 再開時の基準

- Portal benchmark: `bed98bb6-684f-4f19-91b3-0cc8d220a388`
- score: **167,395 / PASSED / deduction 0 / timeout 397**
- capture: `/home/pikachu0310/github/isucon-agent-kit/runs/20260722T021859.404731Z-s1-b7f8c5`
- live source: `a7955d62c9eaa94c0560b6d98518c93d08ef4c61`
- topology: s1=Nginx、s2=registration+condition forward+MariaDB、s3=全conditionの正本+全read
- 主な観測値: condition POST 271,597、登録POST 1,023、graph GET 3,043、condition GET 24,816

判定: POSTを28万件受けても、約900台へ薄く配るため24時間内の各hourに必要なcondition密度を作れず、GraphWorstが支配的だった。condition POSTや登録自体には直接得点がなく、`trendの変化 -> user追加 -> ISU追加` を無制御に回したことが、得点になるread/graphを圧迫していた。

## R1: trendを初回snapshotで固定

- Portal benchmark: `5725fbd6-e476-4e50-adbc-14b184e64f74`
- score: **5,387,452 / PASSED / deduction 0 / timeout 0**
- capture: `/home/pikachu0310/github/isucon-agent-kit/runs/20260722T024757.725796Z-s1-9f1054`
- source: `b8e45a4fbc17a6803c1d77f0f3faaf2a8b07dfd5`
- binary (s2/s3): `3632a9378f81ec69d822775d076dab1e100b5831993ada1aeeb289ea3e4beba9`
- 併施: s2にも`LimitNOFILE=524288:524288`を配備

Portal内訳:

| tag | count |
|---|---:|
| GraphGood | 30,728 |
| GraphNormal | 1,651 |
| GraphBad | 0 |
| GraphWorst | 4,506 |
| TodayGraphGood | 4,557 |
| TodayGraphNormal | 608 |
| TodayGraphBad | 0 |
| TodayGraphWorst | 441 |
| ReadInfoCondition | 12,901 |
| ReadWarningCondition | 1,196 |
| ReadCriticalCondition | 0 |

変化:

- userAdderは全tickで「ユーザーは増えませんでした」
- 登録POSTは1,023から55、condition POSTは271,597から70,589へ減少
- graph GETは3,043から39,085、condition GETは24,816から47,988へ増加
- scoreは167,395から5,387,452へ **約32.2倍**

判定: 仮説は確定。重要なのは受理request総数ではなく、GraphGoodとscored conditionへ変換できた量である。

残った壁:

- s1送信は平均約112,076KB/s（約896Mbps）。icon本文だけで約3.0GiB/60秒
- icon GETは125,541回でほぼ全て200。現行版から旧高得点版のprivate cache/ETagが抜けていた
- s3 CPUは平均約42% busy、s2は約19% busyで、計算資源より先にs1の外向き帯域へ達した

## R2: 2 userだけ増やすbounded ramp + immutable icon cache

- source: `29b557abc13d89471379e5be0df3eb6beff3e0ff`
- binary (s2/s3): `db59734b1f4b7edd7ea126299593885f1fd68bf8110c76ab1726f35887659e0c`
- Portal benchmark: `7405f9f8-1c3d-4e72-96ff-0467a22c99ea`
- score: **5,459,466 / PASSED / deduction 0 / timeout 0**
- capture: `/home/pikachu0310/github/isucon-agent-kit/runs/20260722T031135.925580Z-s1-9cee5c`

変更:

1. initialize後、最初にacceptしたconditionから4.5秒だけtrendを500ms間隔で更新し、その後は永久固定する。
2. 初期51 ISU × 最大21 viewer × 約8〜9世代なので、最初の5秒で約2 userだけ増えることを狙う。3 userの閾値には届かない設計。
3. iconは所有者認可とmemory lookupの後に`Cache-Control: private, max-age=3600`とUUID ETagを付ける。画像に更新routeはなくimmutable。wrong-userへ304を返さない順序を維持する。

実測:

- 最初の5秒で設計どおり「ユーザーが2人増えました」。以後は増加なし
- icon GETは125,541から21,907へ82.5%減少
- s1送信は平均112,076KB/sから77,582KB/sへ低下
- completed graph総数は増えたが、GraphGoodは30,728から28,324へ減り、Normal/Bad/Worstが増えた
- scoreは+72,014（+1.34%）。人口増加の単純比例予測は外れ、condition密度低下が相殺した

判定: icon client cacheは明確に採用。bounded rampも最高点は更新したため現時点では維持するが、人口ではなく完成済みgraphを再利用してscoring loopを速める方が次の本命。

## R3: completed graph/detail/frozen trendのclient cache

- source: `e161d915e6034045e7ac0bc655e80b93175e77db`
- binary (s2/s3): `a93b919f16eebfe542e7c6b0c9d313cba7dd8000feda6e6696043bd269104d89`
- Portal benchmark: `485457ed-3432-4090-a28c-1628cff0c4e3`
- score: **5,507,168 / PASSED / deduction 0 / timeout 0**
- capture: `/home/pikachu0310/github/isucon-agent-kit/runs/20260722T032712.173392Z-s1-41a9e3`
- completed graphだけにprivate cacheを付ける。無条件cacheは使わない
- 安全条件: そのISUのlatest conditionが対象range終了よりさらに仮想24時間以上先にあること
- `GET /api/isu/:uuid`は登録後不変なのでprivate cache
- trendは4.5秒ramp終了後に限りprivate cache
- 同じgraph URLは約39,000 requestに対して約1,100種類。公式Agentはfresh cacheをnetworkへ送らずbodyを復元するが、復元bodyも通常どおり検証・加点される

実測:

- trend requestはR2比74.5%減、detailは54.9%減、graphは3.45%減、総requestは8.0%減
- networkへ来たgraphは39,972件だが、完全URIは1,255種類だけで96.86%が重複
- graph network 2xxは40,915から39,492へ減った一方、Portalのgraph採点回数は45,277から45,801へ増えた。client cacheから復元したbodyも採点されることを実測で確認
- scoreは+47,702（+0.87%）。キャッシュ自体は正しく効いたが、completed判定だけでは進行中の日付の重複を消せない
- s1 CPU busy約29%、s3約36%、s1送信約76,101KB/sで、3台のCPUや1Gbps回線は飽和していない

判定: serverを増やす前に、公式validatorの1秒condition反映猶予に合わせてactive graphを1秒だけclient cacheする。また、completed graphのmarshal済みbodyをinitialize世代内で共有し、signout後の再取得でも再計算しない。2 user追加はGraphGood比率を落としたため完全凍結へ戻す。この3点をR4の一つのbundleとする。

## R4: active graph 1秒cache + completed graph encoded cache + 完全凍結

- active/today graph: `Cache-Control: private, max-age=1`
- completed graph: 従来の長期client cacheに加え、認可後にmarshal済みJSONをgeneration-scoped memoryから返す
- trend: refresh windowを0秒にし、追加userを再び0人にする
- 安全根拠: 公式benchmarkの`ConditionDelayTime`は1秒。fresh cacheが返り得る時間をその猶予より長くしない
- source: `8dc37724d32521bc16eef3799f5725fcbaaff54c`
- binary (s2/s3): `9f169b88712a876496f39097e41f03b4164395fe08f63834f48e917f5c1e4121`
- Portal benchmark: `417c71ac-4739-4788-ba23-c9d934252269`
- score: **6,379,010 / PASSED / deduction 0 / timeout 0**
- capture: `/home/pikachu0310/github/isucon-agent-kit/runs/20260722T062148.370372Z-s1-2ef6c1`

Portal内訳:

| tag | count |
|---|---:|
| GraphGood | 36,111 |
| GraphNormal | 2,875 |
| GraphBad | 0 |
| GraphWorst | 5,460 |
| TodayGraphGood | 5,225 |
| TodayGraphNormal | 830 |
| TodayGraphBad | 0 |
| TodayGraphWorst | 529 |
| ReadInfoCondition | 13,127 |
| ReadWarningCondition | 988 |
| ReadCriticalCondition | 0 |

実測:

- scoreはR3から **+871,842（+15.83%）**。650万点まで残り121,426点
- graphは45,618 network request（うち2xx 45,118）に対し、Portalでは44,446 completed graphと6,584 today graphを採点
- condition POST 70,340、condition GET 50,352。全tickでuser追加0人
- s1/s3 CPU idleはそれぞれ約69%、s2は約81%。s1送信は平均77,449KB/sで、CPU・帯域とも未飽和
- s3 CPU profileで`generateIsuGraphResponse`はR3の3.66秒から0.73秒へ低下

判定: active graphを公式validatorの猶予内だけ再利用する変更が主因となり、大幅にscoring loopを高速化した。次は現バイナリをchampionとして退避し、残り約2%を再測定の分散または同じ安全性を保てる構造改善で詰める。
