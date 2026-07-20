# PISCON 2026: Codex追加10時間・大会内情報限定・最高得点優先実行契約

## ユーザーが送るのは次の1行だけ

```text
/goal /home/pikachu0310/github/piscon2026-observe/prompts/continue-10h-score-first-ja.mdを最初から最後まで読み、この契約を最優先で実行せよ。PISCON公式portalでvalidかつdeduction 0の最終scoreを追加10時間の能動作業で最大化せよ。ただし各runはscoreだけで即棄却せず、有効負荷、単位仕事cost、bottleneck移動を追って複数段の改善として評価せよ。根拠は大会中に参加者が入手できる配布物、現在source、実機、今回の計測と実験だけに限定し、過去解答repo、他teamの解答、終了後の公式解説を使うな。最大3体のread-onlyサブエージェントを直ちに並列起動し、時間・実験量・finalの完了条件を満たす前にgoalを完了扱いにするな。
```

この1行だけを`/goal`へ送り、長い契約本文はこのfileから読む。goalが自動継続されたturnでも、
最初にこのfile、session台帳、score台帳を読み直して再開する。

## 最優先目的

PISCON公式portalの同一benchmark系列で、`valid / deduction 0`を守った最高scoreを取る。
局所metric、変更量、説明資料、現在の構成を保存することは副目的である。大きな構造変更を含め、
配布sourceと実測から得点を桁で変え得る案を自力で設計する。

ただし、公式benchmarkは閉ループである。上流を速くすると利用者、登録、condition、read試行が
増え、その先のresourceを使い切って一時的にscoreが下がることがある。1 runのscore低下だけで
改善を棄却しない。常に次の2本を別々に保護する。

- `score-champion`: 現在のvalid最高score構成。いつでも復元できるmanifestを持つ。
- `capacity-frontier`: score未更新でも、有効負荷を増やした、単位仕事costを下げた、または次のbottleneckを明確に露出した構成。

最終提出は`score-champion`から選ぶが、探索中は`capacity-frontier`の2〜3手先まで直してから
構造案を判断する。局所metricが良いだけの変更と、負荷の段階を一段進めた変更を区別する。

- 第一目標: 公式score 1,000,000以上
- stretch: 1,464,232以上
- 最低条件: session開始時に確認した再現可能最高scoreを明確に更新する

目標未達でも能動時間が残る限り探索を続ける。stretchを同一構成で2回超え、全台reboot後の
finalでもvalidなら早期完了してよい。

## 環境と開始状態

- 計測制御repo: `/home/pikachu0310/github/isucon-agent-kit`
- 改善対象repo: `/home/pikachu0310/github/piscon2026-observe`
- inventory: `/home/pikachu0310/github/isucon-agent-kit/inventory.tsv`
- 公式portal: `https://piscon.trap.jp/`。ログイン済みbrowserを使う
- benchmark時間: 60秒固定
- s1: public `54.65.31.134` / private `10.0.0.26`
- s2: public `54.250.238.3` / private `10.0.0.143`
- s3: public `54.199.121.152` / private `10.0.0.113`
- 現在の実機: s1=Nginx、s2=MariaDB、s3=Go App、final mode
- 現在branch: `agent/piscon-after-prompt-20260719-2304`
- 現在HEAD: `e70329f`
- 現在final: `139,772 / PASSED / deduction 0`
- 今回のPISCON sessionで確認済みの再現可能最高: `161,622`、reboot後`158,997`

開始時に実機、Git、portal historyを再確認し、値が変わっていれば台帳を更新する。上記scoreは
開始地点を示すだけで、既知の構成へ固執する理由にしない。

## 競技終了・追試のhard guard（作業時間より優先）

「10時間」は競技運営の締切を延長しない。最初のserver変更、benchmark、restartより前に、portalの
配布document、当日連絡、現在時刻から次を確認し、`.state/optimization-session.tsv`と実験台帳の先頭へ
絶対時刻で記録する。

- 競技終了時刻
- portalがbenchmarkを受け付けなくなる時刻
- 追試開始時刻と、その間のserver操作可否
- 最終構成を確定すべきhard freeze

hard freezeは、運営指定時刻があればその時刻、なければ競技終了/portal停止/追試開始のうち最も早い
時刻の60分前とする。探索終了はさらに45分前とし、そこからscore champion復元、全台reboot、final
確認、公式benchmarkへ移る。開始時点からhard freezeまで10時間未満なら、10時間契約をそのまま開始
してはならない。残り時間に圧縮した探索・final計画を明示し、finalを間に合わせる。時間要件と運営
締切が矛盾した場合は、運営締切を必ず優先する。

hard freeze以降は禁止する:

- measure modeへの復帰、新規実験、deploy、restart、config変更、log rotation
- 「あと1回だけ」を含む探索benchmark
- score/capacity frontierを理由にしたfinal構成の差し替え

追試開始以降は、SSH、HTTP負荷、profile取得、benchmark、reboot、service/config操作を一切行わない。
運営documentが「serverへのアクセスが追試へ悪影響」としている場合は、軽いread-only確認も行わない。
portalのinstance一覧が消える、対象serverが選べない、rankingが確定表示になる、追試時刻を過ぎる、の
いずれかを観測したら、それを終了状態として扱う。新しいinstanceを作って代替せず、server操作を
停止し、既にlocalへ同期済みの証跡だけで記録を閉じる。

このguardはgoalの継続性・10時間・30 run・stretch・「ユーザー入力なしで停止しない」より優先する。
締切で未達になった項目は、無理に実行せず外部deadlineによる未達として報告する。

## 根拠の境界: 大会で知れることだけ

利用してよい:

- 大会開始時または大会中に運営が参加者へ配布・公開したmanual、規約、FAQ、追加連絡
- 配布application source、schema、config、frontend、初期data、実機image
- goal開始時の現在checkoutにあるsource/config。これを新しい競技の開始状態として扱う
- 3台のservice、process、port、resource、network、package、設定
- PISCON公式portalが返すscore、status、deduction、timeout、error
- 今回取得したaccess/slow log、pprof、fgprof、sar、pidstat、journal、packet/connection情報
- 今回の実験結果、EXPLAIN、test、smoke、現在sessionのGit diff
- Go、Nginx、MariaDBなど一般技術のhelp、man page、公式reference

利用してはいけない:

- `/home/pikachu0310/github/isucon11-ai-agent-2026`を含む、同じ過去問を以前改善したrepo
- 現在のPISCON開始前に作られた解答branch、commit、patch、設計資料、profile、score履歴
- goal開始commitより前のGit history、古いbranchの実装、commit message、diff
- 過去repo由来のE47記述を含む既存`docs/optimization-log.md`の本文
- 他teamのcode、解答、記事、発表、SNS、benchmark結果
- 大会終了後に公開されたISUCON11公式解説、講評、模範実装
- ISUCON11/PISCON固有の攻略を探すweb検索
- 運営が参加者へ配布していないbenchmark/JIA内部source
- modelが学習済み知識として覚えているISUCON11の定石や具体的解法

既存`docs/optimization-log.md`とgoal開始前のGit historyは解法由来情報を含むため開かない。
goal開始後に`docs/optimization-log-contest-only.md`を新規作成し、B0以降の仮説、実験、scoreだけを
記録する。現在checkoutのsourceは開始状態として読めるが、その変更理由を過去履歴から逆引きしない。
公式manualと公式解説を混同しない。大会中に配布されたmanualは使用可、終了後の解説は使用禁止。

## 開始直後の30分

最初の30分以内に次を並行実行する。

1. `$isucon-observability-bootstrap`、計測repoのAGENTS/README、この契約、大会配布manualを完全に読み、
   競技終了・portal停止・追試時刻からhard freezeを先に確定する。
2. 改善repoに新しい専用branchを作る。既存dirty fileを消さず、秘密情報やraw artifactをcommitしない。
3. `.state/optimization-session.tsv`へgoal開始/再開時刻を記録する。15分超のplatform中断は能動時間に数えない。
4. `bin/isuctl mode measure`、`doctor`、`watch-start`を確認する。成功済みの`test-capture`は繰り返さない。
5. 現在構成を変えず公式benchmarkを1回実行し、B0としてscore、benchmark/run ID、commitを記録する。
6. goal開始時checkoutとB0を最初の`score-champion`としてmanifestで保護する。古いcommitを復元しない。
7. `docs/optimization-log-contest-only.md`を新規作成し、以後はこの台帳だけを使う。
8. 配布source、実機、B0計測から、request/state/scoreの現状図を作る。

同時に次の3体を明示的にspawnする。main agentは結果待ちで止まらない。

- Agent A: 配布schema、SQL、initialize、transaction、slow logから、state lifecycleとDB制約を分析する。
- Agent B: 現在App source、pprof/fgprof、allocation、lock、goroutine、HTTP処理から、計算と待ちの制約を分析する。
- Agent C: access log、OS、Nginx、3台network、大会manualから、traffic、timeout、node利用率、外部I/Oを分析する。

各Agentには根拠pathと数値、上位3候補、期待するscoreへの機序、壊れ得る仕様を短く返させる。
main agentだけが編集、deploy、restart、benchmark POST、Git統合を行う。60分ごとと大規模変更の
前後に最新runをfollow-up分析させる。

## 最初に作る4枚の現状図

解法を先に決めず、最初の45分で次をsourceと実測から作る。

### 1. Traffic map

各endpointについて、外部入口、Nginx、App handler、DB/外部service、responseまでを結ぶ。
request数、平均/p95/p99、status、request/response byte、timeout、担当nodeを重ねる。

### 2. State ownership matrix

主要table/cache/stateごとに、作成者、更新者、読取endpoint、正本、再構築方法、initialize時の扱い、
許される反映遅延を整理する。DBが本当にhot pathの正本である必要があるかもsourceから検証する。

### 3. Score critical path

利用者またはデバイスが増え、書き込み、読み取り、得点へ到達するまでの順序を、manual、frontend、
App、access logから推定する。推定と確認済み事実を分け、各段階のlatency/concurrency/timeoutを測る。

### 4. Three-node resource map

各nodeのCPU user/system/idle、memory、network、FD、connection、主要processを同一60秒で比較する。
「serviceを分けた」ことではなく、得点制約を3台で分担できているかを判定する。

この4枚から、最大の制約と、なぜ現在scoreがそこで頭打ちになるかを一文で定義してから実装する。

## 根本的な構造改善の作り方

最初の90分はmicro optimizationより、得点を桁で変え得る構造を優先する。過去解答を再現する
のではなく、現状図から少なくとも5案を発散し、次の表で上位3案を選ぶ。

- どのcritical pathを短くするか
- どの繰り返し処理、同期I/O、全件走査、fan-out、serializationを消すか
- 3台のどのresourceを使うか
- expected score倍率と、その根拠となる件数×時間
- correctness invariant、initialize、再起動復元
- 最小smoke、deploy時間、rollback時間

候補は少なくとも次の3系統から1案ずつ公式benchmarkまで通す。

1. Data/state構造: 正本、cache、precompute、batch、data representation、永続化境界を変える。
2. Request critical path: 最多または得点開始を止めるendpointの処理、外部I/O、順序、protocolを作り直す。
3. Three-node topology: traffic/state/resourceの観測に基づいて仕事を再配置または分散する。

ここで具体的な分散方法やcache方式を先に仮定しない。sourceから必要な整合性を抽出し、現在の
計測で余っているresourceと詰まっているresourceを結ぶ。大規模変更は許可されており、必要なら
handler、storage、同期方式、service topologyを全面的に置き換えてよい。

開始90分以内に最有力の構造候補をdeployし、最初の公式benchmarkを通す。配備漏れ、port、
protocol、seedの問題で失敗した場合は構造を即棄却せず、原因を直してvalid runを1回取る。

## 公式benchmark loop

1. `runs/latest`のmeta/errors、ALP、slow summary、CPU pprof、fgprof、sar、pidstat、score内訳を横断する。
2. 実装前にgoal開始後の`docs/optimization-log-contest-only.md`と新branchのGit logだけを検索し、既出実験を重複させない。
3. 仮説、scoreを上げる機序、変更bundle、期待倍率、直接metric、risk、採否条件を短く書く。
4. build/test/race/config testと、initialize、主要write/read、invalid inputをsmokeする。
5. active benchmark/capture中はdeploy、restart、mode変更、log rotateをしない。
6. portal POSTはpreflight後ちょうど1回。曖昧でも自動retryせず、accepted benchmark IDを回収する。
7. benchmarkの60秒中に次候補のsource調査、diff、test、台帳検索を進める。
8. score、有効負荷量、単位仕事cost、resource飽和、bottleneck位置の5軸で判定する。大規模変更と5%以内の差は同一構成でもう1回測る。
9. score更新なら`score-champion`を更新する。score未更新でも後述の条件を満たせば`capacity-frontier`としてbranch/configを保持し、次のbottleneckを直す。不採用時だけrollbackする。

5実験ごとに`score-champion`を無変更で1回測り、run varianceを推定する。登録数、成功write数、
主要read、timeoutなど、sourceとlogから得点負荷を表す指標を選び併記する。

## 閉ループbenchmarkとfrontierの採用規則

- 最終目的は公式scoreだが、各runをscore単独で即決しない。
- `score-champion`は`valid / deduction 0`の公式最高score構成として常に保護する。
- 利用者/登録/対象ISU数、condition 202、成功read、Graph/Readなど、得点負荷を表す件数をsourceとlogから選ぶ。
- endpoint別count/latency/statusと、node別CPU user/system/idle、network、FD、DB/lock/queueを同じrunで比較する。
- 最高構成で再現していないfailureを理由に、低score構成へ入れ替えない。
- reliability修正は最高構成でfailureを再現してから入れ、同条件A/Bでscore低下がvariance内か確認する。
- 最新commitと最高commitを混同せず、最高score commit/config/topologyをmanifestで保護する。
- invalid高得点は採用しないが、potentialを示すためcorrectness修正を最優先候補にする。

scoreが下がっても、次をすべて満たす変更は`capacity-frontier`として残す。

1. 意味上の不整合、データ破壊、仕様違反を新しく生んでいない。増えた負荷によるtimeout/5xx/deductionは、下流飽和と因果を示せるなら探索中だけ許容する。
2. 有効な利用者、登録、condition/readのattemptとsuccessが増えた、または同じ仕事あたりCPU/latency/SQL/lockが明確に減った。
3. 増えた負荷により、別endpoint/node/resourceが新しく飽和し、timeout/5xx/queue増加をaccess log、profile、OS計測で説明できる。
4. 次に直すdownstream bottleneckと、それを直すと総scoreへ変換される因果仮説がある。

`capacity-frontier`では上流改善をrollbackせず、同じbranchでdownstream bottleneckを最大3実験または
能動60分まで追う。例えば登録増加後にconditionが飽和したなら、登録改善を戻す前にcondition側を
直す。複数段のbundleとして`score-champion`と再比較し、score回復だけでなく負荷生成量、単位cost、
飽和位置が仮説どおり動いたか確認する。
有効負荷が20%以上増えた大規模構造変更は、進展を30分ごとに確認しながら最大5実験/90分まで延長できる。

次の場合はcapacity改善ではなく局所metric改善と判断して棚上げする。

- scoreも有効負荷量も単位仕事costも改善しない。
- 負荷量が増えた理由を変更との因果で説明できない。
- 意味上のcorrectness問題またはデータ破壊を生んだ。単なる下流過負荷のtimeout/5xxはここに含めない。
- 次のbottleneckが測定できず、3実験/60分で総scoreへつながる兆候がない。

## 作業速度とpivot

- `.state/optimization-session.tsv`へ能動作業区間、benchmark開始、構造系統、scoreを追記する。
- 15分超のplatform中断・goal休止は10時間へ数えない。再開時は残り能動時間を計算する。
- setup後は平均4公式benchmark/能動作業1時間、合計30回以上を最低目安にする。回数稼ぎはしない。
- benchmark後20分以内に次を開始する。30分空く場合は理由と進捗、60分空く場合は実装を分割する。
- 1時間ごとに最高score、capacity frontier、有効負荷量、単位cost、構造別結果、残り候補の期待倍率を台帳へ1段落で記録する。
- 2時間score更新もcapacity frontier進展もなければ、同じ系統のmicro optimizationを止め、別の構造系統へpivotする。
- 3時間時点で300,000未満なら、4枚の現状図とcritical path仮説をデータから作り直す。
- 6時間時点で700,000未満なら、局所改善を全面停止し、未検証の根本構造へ移る。
- 9時間15分まで新規探索し、最後の45分をbest復元とfinalへ使う。

`/goal`のelapsed表示と台帳の能動時間を1時間ごとに照合し、短い方を残り時間へ使う。
新規公式benchmark 30回＋大規模構造3系統は10時間の代替ではなく最低実験量である。

## rollbackと安全

- 大規模変更前に、3台すべてのsource/binary、Nginx、MariaDB、systemd、environment、service role、SQL全fileをchecksum付きで退避する。
- Git対象外の初期dataを削除・上書きせず、size、hash、baseline row countをmanifestへ入れる。
- raw log、profile、binary、inventory、鍵、Cookie、認証情報をGitへ入れない。
- instance、AMI、volume、security groupを削除しない。公開portを新規に広げない。
- rollback後はhashだけでなく、initialize、row count、80/443、App、DB、private接続を確認する。

## hard freeze前の最後の45分

1. 新規探索を止め、`score-champion`の最高score commit/config/topologyを復元する。capacity frontierや最新HEADを無条件に選ばない。
2. 全test、build、config test、smoke、初期data manifest、service enable/active、binary/config hashを照合する。
3. `bin/isuctl final`を実行して全3台をrebootする。
4. final mode、計測停止、80/443、必要service、initializeとstate再構築を確認する。
5. 公式benchmarkを実行する。最高scoreからvarianceを超えて落ちた場合、原因を確認し、時間内なら同一構成をもう1回測る。
6. final score、session最高、benchmark ID、deduction、timeout、commit、topology、再現手順を記録してpushする。

この45分をgoalの経過時間だけから計算しない。goal開始から9時間15分より、運営deadlineから逆算した
探索終了時刻が早ければ、後者で即座に探索を止める。

## goal完了条件

次をすべて満たすまで`complete`にしない。

- 運営deadlineまで十分な時間がある場合は9時間15分の能動的な探索と45分のfinal作業を行った。
- deadlineまで10時間未満で開始した場合は、記録済みhard freezeまでに圧縮計画とfinalを完了した。
- 新規公式benchmark 30回以上と、Data/state・critical path・three-node topologyの3系統を実測した。
- 例外はstretch 1,464,232を同一構成で2回超え、reboot後finalもvalidの場合だけ。
- session中の最高valid scoreと最終reboot後scoreを区別している。
- 最終構成がdeduction 0で、Gitと開始imageから再現できる。
- 全実験、失敗、rollback、score、benchmark/run ID、残った候補を記録した。
- 3台のroleと、構造変更がscoreへ効いた機序を今回のsource・計測だけで説明できる。
- 禁止した過去repo、他team解答、終了後公式解説を参照していない。

ユーザー入力が不可欠な権限変更・破壊的操作以外では停止しない。途中でfinalが通っても、残り
能動時間と未検証の高期待値候補がある限りmeasureへ戻し、計測、構造仮説、実装、公式benchmarkを続ける。
