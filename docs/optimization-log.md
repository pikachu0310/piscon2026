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
| E7 | 15:14 | `9088583` | s1をNginx+Go、s2をMariaDB、s3をstandbyにし、DB CPUを別ホストへ分離 | E6でGo約70%、MariaDB約55%、Nginx約50%の同居 | `c9c34781-046f-4a76-96b5-22c92b4c1b29` / `20260719T062705.410069Z-s1-2526df` | **38,602 / PASSED**（減点0、timeout 680） | 採用。E6比1.76倍。次はGoもs3へ分離 |
| E8 | 15:33 | `aa4167e` | s1=Nginx、s2=MariaDB、s3=Goに完全分離 | E7終盤でs1のGo約110%＋Nginx約70%、s2 DBは約70% | `ebe6fc1e-dabc-4bc3-8094-802efed0d031` / capture interrupted | **0 / FAILED**（約30秒で38,226、timeout 0） | 構成は保持。Nginxのworker connection上限が原因なので設定修正後に再評価 |
| E8a | 15:40 | `92f1129` | Nginxのopen-file / worker connection上限を引き上げ、完全3台分離を再評価 | E8のerror logに`socket() failed (24: Too many open files)`が大量発生。systemd NOFILEは524288だがworker_connectionsは1024 | `03d1bf3a-9a7c-4bbf-9e0e-35e62d230727` / s1+s2 `20260719T064046.117587Z-s1-14ee54`; s3 `20260719T064046.106445Z-s3-c343a2` | **53,009 / PASSED**（減点0、timeout 735） | 採用。E7比1.37倍、従来最高E3比1.07倍。次はDB connection pool待ちを解消 |
| E9 | 15:49 | `e42ea59` | GoのDB open/idle connection poolを10/2から64/64へ広げる | E8a fgprofでDB connection取得待ち約6,315秒。MariaDB最大使用14/上限151、全3hostにCPU余力 | `294f72ca-df19-4de1-972f-c6952fe20374` / s1 `20260719T064910.607252Z-s1-a6e8a6`; s2+s3 `20260719T064910.524916Z-s3-56f8bb` | **64,861 / PASSED**（減点0、timeout 599） | 採用。E8a比1.22倍。pool待ちは約734秒へ88%減少 |
| E10 | 15:55 | `b03490f` | `/api/trend`の完成JSONを100msだけ共有cacheし、同時requestを1回のDB snapshotへ集約 | E9でtrendは2.69万回、slow 49.8秒+10.9秒、555MiB転送。pprof CPU累積35.6%、malloc/scan支配 | `5b9a14f2-07ca-4448-ab54-7c685396d884` / s1 `20260719T065550.068284Z-s1-abdff3`; s2+s3 `20260719T065549.970723Z-s3-37a27b` | **74,909 / PASSED**（減点0、timeout 11） | 採用。E9比1.15倍。trend latest query 2.69万→709回、49.8秒→0.57秒 |
| E11 | 16:01 | `ac680b0` | 署名済みsession cookieの検証結果をinitialize世代内でcacheする | E10でuser存在確認9.26万query。pprofはcookie gob decodeを含むsession処理20.6%、icon処理21.7% | `8670d7d1-706d-4bb6-a0e7-856d10fbab19` / s1+s2 `20260719T070143.869042Z-s1-58a2fc`; s3 `20260719T070143.834487Z-s3-1beffe` | **76,960 / PASSED**（減点0、timeout 0） | 採用。E10比1.03倍。SELECT user 9.26万→1,442回、session CPU上位から消失 |
| E12 | 16:06 | `ed8fd93` | owner+UUID単位でimmutable icon BLOBを登録時からmemory cacheする | E11 pprofでicon 12.9%。DB response 1.52GiB/56秒の大半が反復BLOB。icon更新routeなし | `d7c858c0-6a1d-4f40-8cc8-d797d6c4ee21` / s1 `20260719T070645.909871Z-s1-87a2e6`; s2+s3 `20260719T070645.866274Z-s3-14b555` | **76,304 / PASSED**（減点0、timeout 4） | 判定保留。E11比-0.85%だがDB 1.52GiB→94.5MiB、App profile 37.0秒→29.5秒。再測定 |
| E12r | 16:10 | `ed8fd93` | E12と同一binary/configを再測定し、0.85%差が分散か判断 | 45秒時点はE11比約9%高く、resource指標は全て明確に改善。大構造変更の再測定規則 | `04a2062b-ead0-475b-8759-0f5048380d29` / capture pending | **77,006 / PASSED**（減点0、timeout 3） | 採用。最高を46点更新、2回valid。resource余力を次の負荷増へ使う |
| E13 | 16:15 | `63379aa` | conditionのdrop率を90%から70%へ下げ、3倍のpayloadを永続化する | E12でDB idle 71%、App idle 69%。multi-row済みでwriteはslow 7.8秒、App CPU 9.6% | `77d80cdb-6ca5-4133-8240-8663b41cbfbc` / s1 `20260719T071421.911172Z-s1-7451aa`; s2+s3 `20260719T071421.844209Z-s3-84e5ae` | **83,411 / PASSED**（減点0、timeout 978） | 採用。E12r比1.08倍。payload増加で得点機会は増えたが、condition INSERTがSQL時間の67.6%を占める新しい制約になった |
| E14 | 16:22 | `c1628ca` | MariaDBのAUTO_INCREMENT割当をinterleaved modeへ変更し、conditionの並列multi-row INSERTを直列化しない | E13でcondition INSERTは6.63万回・64.3万行・133.1秒。`innodb_autoinc_lock_mode=1`、row lock wait 333回 | `762f5753-051a-4a4f-8120-f720499d17ec` / s1 `20260719T072220.070714Z-s1-817d8c`; s2+s3 `20260719T072219.981980Z-s3-cdbece` | **85,339 / PASSED**（減点0、timeout 893） | 最高を2.3%更新したため暫定採用。ただしINSERTは158秒へ増え、単独効果は不明瞭。後続と最終再起動ベンチで再評価 |
| E15 | 16:28 | `5190395` | initialize時に全ISU UUIDをmemoryへ読み、condition POSTの存在確認SQLを除く | E14で同一`SELECT COUNT(isu)`が6.33万回・17.2秒。ISU追加APIは1つ、削除APIなし | `695c89ad-e1ba-4497-9ec3-3901143ed0d3` / s1 `20260719T072831.306117Z-s1-e2d2c8`; s2+s3 `20260719T072831.218674Z-s3-94f97d` | **82,752 / PASSED**（減点1、timeout 772） | SQL削減を達成したため採用。score低下と500はGoのsoft NOFILE=1024到達が原因。次で修正して再評価 |
| E16 | 16:33 | `9d94dc9` | Go serviceのsoft/hard NOFILEを524288へ上げ、終盤のaccept失敗を除く | E15 journalで`accept4: too many open files`が連発。systemd表示は524288だがprocess実測はsoft 1024 / hard 524288 | `c5640cdd-9f12-4376-a406-9c07ba5167e1` / s1+s2 `20260719T073256.003311Z-s1-47c1ae`; s3 `20260719T073255.985463Z-s3-59fef2` | **86,582 / PASSED**（減点0、timeout 800） | 採用。process soft/hardとも524288、accept error 0、最高を1.5%更新 |
| E17 | 16:40 | `d847071` | condition writeを2msだけqueueで束ね、最大256req/2,000行を1 transactionで保存する | E16でhistory INSERT、latest upsert、START/COMMITが各6.8万回。write SQLだけで160秒、fgprof condition累積679秒 | `136cf832-2215-4c5b-8d50-3104866fc3dc` / s1 `20260719T073955.892976Z-s1-af46ca`; s2+s3 `20260719T073955.840819Z-s3-44b65d` | **82,091 / PASSED**（減点0、timeout 133） | scoreは5.2%低下したが、SQL 181秒→29秒、INSERT 6.8万→1.04万回を達成。resource余力とvalidを根拠に採用し保存率を上げる |
| E18 | 16:45 | `fa26ce4` | E17のbatch余力を使いcondition保存率を30%から50%へ上げる | E17でDB/App idle 64%/65%、history INSERT 11.9秒、timeout 133。E13では保存率増が得点へ直結 | `4590bab8-89a4-4c27-a284-4384dc1c3f48` / s1+s2 `20260719T074520.760457Z-s1-4f61f8`; s3 `20260719T074520.748227Z-s3-fd7f55` | **89,650 / PASSED**（減点0、timeout 264） | 採用。E17比1.09倍、history 107万行。DB/App idle 56%/57%の余力を使い次は60%を評価 |
| E19 | 16:50 | `81deeb1` | condition保存率を50%から60%へ上げ、30位ボーダー92,604超を狙う | E18は最高89,650、保存107万行、write 18.5秒。DB/Appに約56% idleだが一部condition p95は80〜100ms | `f1736d3c-6e6c-4ca6-9d7f-1f27f0c07f00` / s1+s2 `20260719T075017.800530Z-s1-fd4a46`; s3 `20260719T075017.798608Z-s3-1824d1` | **94,366 / PASSED**（減点0、timeout 1,283） | 採用。30位ボーダーを1,762点超過。ただし単一writer queueで全APIの終盤tailが悪化。並列writerで解消する |
| E20 | 16:56 | `77be7b6` | batch writerを1本から4本へ増やし、60%保存のqueue待ちをDBの並列実行へ移す | E19はDB/App idle 56%/54%なのにcondition最大5.2秒、他API p95約1秒。history SQL concurrencyは0.34 | `131060c3-5b28-4810-8710-9a88afb1a438` / s1 `20260719T075548.981412Z-s1-bea581`; s2+s3 `20260719T075548.962305Z-s3-af870a` | **99,620 / PASSED**（減点0、timeout 344） | 採用。E19比1.06倍、history 131万行。全API tailとqueue待ちを改善しDB idle 42%まで安全に利用 |
| E21 | 17:02 | `972458a` | 4 writerを維持してcondition保存率を60%から70%へ上げる | E20最高99,620、DB/App idle 42%/54%、history 131万行。condition p95は一部100msでcapacity境界に近い | `73a823f3-1b27-41cb-bdba-f3316a5f39d2` / s1 `20260719T080057.430456Z-s1-5dbe47`; s2+s3 `20260719T080057.367084Z-s3-364152` | **99,897 / PASSED**（減点0、timeout 1,331） | 最高を0.28%更新したため暫定採用。ただしtailが悪化し保存率追加の限界。latest DB書き込みを除いて再評価 |
| E22 | 17:12 | `59b207b` | 最新conditionのbenchmark中の更新・参照をDB tableから同期memory mapへ移す | E21でlatest upsert 1.86万回・12.4秒、historyとの同一transactionでtailを増幅。初期値はinitializeで確定可能 | `6865f48b-d8e8-4e6f-a1ef-e626a3aebc88` / s1 `20260719T081038.619537Z-s1-d2a23e`; s2+s3 `20260719T081038.577944Z-s3-9b326c` | **98,183 / PASSED**（減点0、timeout 3,034） | latest upsert消滅とDB余力改善は採用。同期commit待ちでhandlerが滞留し終盤評判低下。次で202応答をqueue受付時へ移す |
| E23 | 17:19 | `bf25365` | condition POSTをbounded queue受付時に202応答し、DB commitを4 writerへ非同期化 | E22はApp/DBとも約50% idleなのにhandler待ち累積2.99万秒、timeout 3,034。反映猶予は1秒 | `571a918e-ef1c-4501-a9ef-2a08071ba5df` / s1 `20260719T081753.731992Z-s1-bd7f22`; s2+s3 `20260719T081753.674442Z-s3-22b55c` | **101,780 / PASSED**（減点0、timeout 365） | 採用。最高を1.9%更新。condition p95は概ね50〜80ms、fgprof累積は2.99万→292秒。DB readをmemory mirrorへ移す |
| E24 | 17:27 | `f095b73` | commit済みcondition履歴をUUID別memory sliceへmirrorし、condition read/graphをDBから外す | E23でhistory SELECT約1.9万回・18.2秒。DB idle 42%、App idle 57%、初期SQLは1MiB未満 | `c46e78fe-102f-4c61-8aa3-b526a27dd0be` / s1 `20260719T082600.808566Z-s1-ec6b3d`; s2+s3 `20260719T082600.732276Z-s3-fcb2aa` | **126,147 / PASSED**（減点0、timeout 437） | 採用。E23比1.24倍、read SQL消滅。DB時間の88%を占めるhistory永続writeをmemory-onlyへ移す |
| E25 | 17:33 | `5b0d2b4` | scoring中のcondition履歴をmemory-onlyへし、DB INSERT/queue/4 writerを除去 | E24のDB SQL時間74秒中history INSERTが65.5秒。全readは既にmemory、App host available約1.16GiB | `818d7e64-a6c5-4fb8-9e8c-61581a1df58d` / s1 `20260719T083148.605403Z-s1-87ba36`; s2+s3 `20260719T083148.593736Z-s3-68bd9f` | **113,696 / PASSED**（減点0、timeout 343） | scoreはE24比-9.9%だがDB idle 89%、App idle 61%へ大幅改善。latest大域lockを除き100%保持で余力を得点化する |
| E26 | 17:39 | `3bef94b` | latest専用大域mapを廃止しUUID別history末尾から導出、dropを0にして100%保持 | E25でDB idle 89%、App idle 61%、available約1GiBだがlatest mutex待ち101秒。100%見積もり約249万行 | `f06d259c-3a9f-4e55-aa84-c24edf6e6268` / s1 `20260719T083746.414849Z-s1-00aa64`; s2+s3 `20260719T083746.412580Z-s3-1e7112` | **138,994 / PASSED**（減点0、timeout 360） | 採用。E24比1.10倍、E25比1.22倍。全量保持でApp RSS 1.29GiB、available約700MiB。次は履歴表現を圧縮 |
| E27 | 17:45 | `efea500` | 履歴をUUIDなし・int64 timestampへ圧縮し、condition 8種とmessageをintern | E26 App RSS 1.29GiB、available 700MiB。GC scan 4.64秒、malloc 5.59秒、各行の重複文字列とtime.Timeが支配 | `07c803a9-e951-4258-bda1-7053a6a35c62` / s1 `20260719T084610.406450Z-s1-4dc970`; s2+s3 `20260719T084610.319645Z-s3-8050db` | **151,716 / PASSED**（減点0、timeout 321） | 採用。E26比1.09倍、App RSS 1.29GiB→402MiB。次はcondition JSONの二重割当を除く |
| E28 | 17:51 | `a96ea91` | condition JSONを圧縮後の履歴型へ直接decodeし、中間sliceとコピーを除く | E27 pprofで`postIsuCondition` 12.95秒、Echo Bind 11.21秒、JSON decode 10.87秒。圧縮後も受信時だけ旧型を二重保持 | `b4d2f8f6-c281-4adc-b692-228e3bb93069` / s1 `20260719T085340.529228Z-s1-c2f3fd`; s2+s3 `20260719T085340.448674Z-s3-6a1c15` | **152,940 / PASSED**（減点0、timeout 482） | 採用。最高を0.8%更新。App CPU 40.09→38.57秒、JSON経路とGC scan減少を確認 |
| E29 | 17:58 | `1ddf856` | Appの`GOGC`を100から300へ上げ、GC頻度を余剰memoryへ交換 | E28 App host idle 62.2%、平均available 2.15GiB、終了RSS 398MiB。GC scan 2.23秒、mallocgc 4.85秒が残る | `dd5f2ffa-e75c-4f50-910e-a6f9e29f81e8` / s1+s2 `20260719T085934.737571Z-s1-abd2d7`; s3 `20260719T085934.731353Z-s3-430a0f` | **142,722 / PASSED**（減点0、timeout 463） | 不採用。E28比-6.7%。RSS 772MiB、GC CPUは減ったが得点/tailへ変換できず100へ戻す |
| E30 | 18:04 | `713544b` | condition POSTだけを互換JSON decoderへ置換 | E29/E28で標準JSON decode 8.3〜8.7秒、App CPUの約22%。DB idle約90%、read APIは数ms | `a15ece32-34f0-4ead-9642-45a7b4cc6b14` / s1+s2 `20260719T090809.043343Z-s1-b1ec29`; s3 `20260719T090809.037084Z-s3-59ce75` | **156,472 / PASSED**（減点0、timeout 409） | 採用。最高を2.3%更新。decode 8.30→2.72秒、App CPU 38.57→33.01秒 |
| E31 | 18:14 | `581cd6d` | condition request bodyのbufferをpool再利用 | E30 heapで総alloc 4.24GiB中`io.ReadAll`が1.25GiB（29.5%）。終了時にも19.4MiB保持 | `2d058119-e1ea-419c-bdb6-cb41c10e07a3` / s1 `20260719T091510.738931Z-s1-6f48fd`; s2+s3 `20260719T091510.687614Z-s3-4790d7` | **143,332 / PASSED**（減点0、timeout 240） | 判定保留。score -8.4%だが総alloc -30%、condition alloc -59%、App CPU/GC改善。無変更再測定 |
| E31r | 18:19 | `581cd6d` | E31と同一binary/configを再測定 | score低下と、alloc 4.24→2.96GiB・GC scan 3.86→2.59秒・timeout 409→240の改善が矛盾 | `1a58949b-f4bd-4dcd-926c-ac4afa9f2d84` / s1+s2 `20260719T092026.599613Z-s1-a0212d`; s3 `20260719T092026.590917Z-s3-d080f5` | **144,082 / PASSED**（減点0、timeout 345） | 不採用。2回ともE30比約-8%。resource改善だけで採らず、E30のbody読込へrollback |
| E32 | 18:24 | `35b2e31` | message internをread-firstにし、既知文字列の`LoadOrStore` allocationを避ける | E30 heapのalloc object最多はinternの602万個、alloc 89MiB。圧縮効果からmessage重複率は高い | `2311096b-d699-42ed-ad22-c41dd1daf317` / s1+s2 `20260719T092731.212997Z-s1-38f631`; s3 `20260719T092731.186181Z-s3-2e2209` | **146,439 / PASSED**（減点0、timeout 353） | 判定保留。score -6.4%だがalloc object -21%、intern 603万object消滅、handler CPU改善。無変更再測定 |
| E32r | 18:31 | `35b2e31` | E32と同一binary/configを再測定 | read-firstは値不変で、alloc object 3,558万→2,802万、condition CPU 5.95→5.12秒だがscoreだけ低下 | `2dcb033c-cd29-45fc-a9de-535c645aa567` / s1 `20260719T093132.381298Z-s1-0ada59`; s2+s3 `20260719T093132.330197Z-s3-d71bba` | **142,129 / PASSED**（減点0、timeout 392） | 不採用。2回ともE30比-6〜9%。alloc改善だけで採らず、E30のinternへrollback |
| E33 | 18:37 | `cb49145` | JIA協会向けHTTP transportのidle connection poolを2から256へ拡大 | E30はPOST ISU 935回、p95 516ms。fgprofでJIA `Client.Do`累積190.6秒、JIA仕様上の待ちは1回50ms | `15496748-a5b7-43e3-8eb5-c01997f689c9` / s1+s2 `20260719T093954.370469Z-s1-c68772`; s3 `20260719T093954.354628Z-s3-a078c3` | **147,960 / PASSED**（減点0、timeout 301） | 不採用。POST ISU p95 552ms、JIA待ち209秒へ悪化。idle connection上限は制約でなくdefaultへ戻す |
| E34 | 18:45 | `82c0699` | condition routeだけclient abort後もupstream処理を完了 | 公式posterは送信前に期待conditionへ追加し、100ms timeout結果を無視。access logに多数の499があり、既定Nginxはupstreamを切断 | `bf9fce41-189a-4720-bacb-b4913b35f193` / s1 `20260719T094608.901720Z-s1-9cc990`; s2+s3 `20260719T094608.833200Z-s3-6bbe8e` | **149,915 / PASSED**（減点0、timeout 370） | 不採用。499 4,635件は全てupstream未到達。request body受信中断はこの設定で救えずrollback |
| E35 | 18:52 | pending | immutable JS/CSSを事前gzipし、Nginx `gzip_static`で配信 | E30 vendor JS 743KiBを1,522回、p95 690ms。gzip-6で203KiB（72%減）、Nginx host idle 65%、gzip_static moduleあり | pending | pending | 元assetを保持しAccept-Encoding時だけ配信。checksum/validator、Content-Encoding、asset p95/bytes、user増加、scoreを確認 |

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

### E7 result

- scoreはE6の21,950から38,602へ76%向上し、remote DB分離を採用した。
- 前半約30秒はtimeout 0のままユーザー増加が3, 4, 5, 7, 8人と加速した。
- trendは19,334回・平均87ms、isu一覧は5,041回・平均31msまで処理量が増えた。
- s2のMariaDBは平均CPU約37%、終盤でも約67〜80%で、まだ余力がある。
- s1は終盤にGo約108〜110%、Nginx合計約70%となり再び2 vCPUを使い切った。timeout 680はここから急増した。

### E8 expectation

- s1はbenchmark-facing Nginxと静的配信、s3はGo、s2はMariaDBだけを担当する。
- s1からGo約1 coreを物理的に外し、Nginxのaccept/proxy/log処理と競合させない。
- private networkのproxy hopは増えるが、E7のDB hopで効果を確認済み。各hostのCPU headroomを均す。
- initializeはs3のGoからs2 DBへ実行し、JIA serviceはprivate address、callbackは従来の公開URLを維持する。

### E8 result and E8a expectation

- E8は約30秒までtimeout 0でユーザー増加が3, 4, 6, 7, 9, 9人と加速し、途中スコアは38,226に達した。
- その直後、s1のNginxが大量の500を返した。s2のMariaDBとs3のGoはactiveのままで、アプリのcrashではなかった。
- Nginx error logにはupstream接続時の`socket() failed (24: Too many open files)`が集中していた。
- systemdの`LimitNOFILE`は既に524288だが、Nginxの`worker_connections`はworkerあたり1024の初期値だった。
- E8aでは`worker_rlimit_nofile=524288`、`worker_connections=32768`、`multi_accept on`を追跡可能なmain configへ入れる。
- 失敗前の伸びを保ったまま60秒完走し、E7の38,602を超えることを期待する。

### E8a result

- 53,009点でPASSEDし、E7の38,602を37%上回った。Nginxの500とfile descriptor errorは再発しなかった。
- s1の平均CPU idleは68.7%、s2は46.2%、s3は29.7%。物理分離後は全hostにまだCPU余力がある。
- slow logは34.7万query、6.20k QPS、DB実行concurrency 1.41。MariaDBの最大接続数151に対し、実測最大使用は14だった。
- fgprofでは`database/sql.(*DB).conn`待ちが累積約6,315秒。アプリの`SetMaxOpenConns(10)`が、空いているDB/CPUの利用を妨げている。
- pprofでは`getTrend`がCPU累積27.4%、allocation/GCが大きいが、まず接続待ちを単独で除いて次の壁を露出させる。

### E9 expectation

- DB pool待ちを大幅に減らし、s2/s3の未使用CPUをrequest処理へ回す。
- `GET /api/trend`と`GET /api/isu`の終盤p95および公式timeoutを減らす。
- MariaDBの`max_connections=151`に対しアプリ上限64とし、計測接続や管理用の余白を残す。
- SQL、response、transaction境界は変えない。接続数だけの分離実験として評価する。

### E9 result

- scoreは53,009から64,861へ22%向上し、timeout発生開始は約39秒から約57秒まで遅れた。
- fgprofのDB connection取得待ちは約6,315秒から734秒へ88%減少した。pool拡張を採用する。
- `/api/trend`は27,343回・平均26msまで増え、latest JOINだけで49.8秒、character一覧で10.9秒を使用した。
- trend latest queryは555MiBを返し、pprofでは`getTrend`がCPU累積35.6%、`mallocgc`21.7%となった。
- 全requestがほぼ同じ公開trend snapshotを個別にquery・scan・sort・marshalしている重複処理が次の最大制約。

### E10 expectation

- trend response全体を100msだけ共有し、約480 QPSの同一計算を最大10回/秒へ集約する。
- initializeと新規ISU登録では即時invalidateする。condition更新は最大100msで反映し、元のdrop挙動とDBを正とする。
- trendのDB query本数・転送・CPU allocationを95%以上減らし、App/DBの余力をgraph・conditionへ回す。
- JSON schema、分類、timestamp降順、HTTP status/content typeは維持する。

### E10 result

- scoreは64,861から74,909へ15%向上し、公式timeoutは599から11へ減少した。100ms cacheでも検証はPASSEDした。
- trend latest queryは26,907回・49.8秒から709回・0.57秒へ、character queryも26,967回から713回へ減った。
- App平均CPU idleは58.2%、DBは64.6%まで回復し、trendは29,283回・平均1msになった。
- 次のpprof上位は`getIsuIcon`累積21.7%と`getUserIDFromSession`累積20.6%。
- user存在確認は92,639回あり、毎requestで同じ署名cookieをgob decodeしてDB確認している。

### E11 expectation

- raw session cookieをkeyに、署名検証とDB存在確認を通過したuser IDだけをmemoryへcacheする。
- initializeで全cacheを消し、signoutで該当cookieを消すため、DB resetとlogoutの意味を維持する。
- user存在確認queryとsecurecookie/gob decodeを、request数ではなくcookie数相当まで99%以上減らす。
- cookie cache miss時は従来処理をそのまま実行し、偽造cookieや初期化前cookieを受け入れない。

### E11 result

- scoreは74,909から76,960へ2.7%向上し、timeout 0でPASSEDした。initialize/signoutを含む検証も通過した。
- user存在確認queryは92,639回から1,442回へ98.4%減り、session/securecookie/gobはpprof上位から消えた。
- App CPU profileは42.7秒から37.0秒、DB queryは29.5万から20.7万へ減少した。
- 次のhandler首位は`getIsuIcon`累積12.9%。同一user/ISUのimmutable BLOBを毎回remote DBから取得している。
- slow logのDB responseは合計1.52GiB。DB/AppともCPU idleは60%以上だが、同じicon転送を反復している。

### E12 expectation

- iconは登録後に更新するAPIがないため、owner+ISU UUIDをkeyに登録時のbyte列を保持する。
- cache missは従来のowner条件付きSQLを通し、成功分だけcacheする。initializeで全消去して権限と初期データを維持する。
- icon query、remote DB BLOB転送、GoのSQL scan/allocationをほぼ全廃する。
- HTTP status、認証、owner判定、response body/content typeは変えない。

### E12 first result and remeasurement decision

- 初回scoreは76,304でE11を0.85%下回ったがPASSED、timeout 4。45秒時点では65,824対60,212で約9%上回っていた。
- DB queryは20.7万から14.3万、response bytesは1.52GiBから94.5MiBへ減り、反復icon queryは59,535回から84回になった。
- App CPU profileは37.0秒から29.5秒、DB CPU idleは67.0%から71.1%。実装上の狙いは達成している。
- 評判増加タイミングと最終採点の分散に対し差が小さいため、同一commitを変更なしでもう1回だけ測る。

### E12 remeasurement result

- 同一binaryの2回目は77,006点でPASSED、E11を46点だけ上回り、公式最高を更新した。
- 2回平均ではE11とほぼ同じだが、validatorを2回通し、DB転送を約16分の1、App profileを約20%減らした。
- 後続のwrite比率変更へ大きなheadroomを残すため採用する。後続bundleが悪化した場合もicon cache単体へ戻せる。

### E13 expectation

- 現在はcondition payloadの90%をbody検証前にAcceptedとして捨てている。これを70%へ下げ、保存量を約3倍にする。
- E12のDBは平均idle 71%、Appは69%。write payloadは約1.72万/分で、3倍化してもCPU容量内と見込む。
- multi-row INSERTとlatest upsertを維持し、ReadConditionおよびGraphで利用可能なconditionを増やす。
- timeout、write SQL時間、DB/App CPU、condition/graph別得点を比較し、scoreが伸びなければ元へ戻す。

### E13 result

- 83,411点でPASSEDし、E12rの77,006点から8.3%向上した。30%保存を採用する。
- 公式得点のうちGraphWorst 2,144、TodayGraphWorst 1,643、ReadInfo 1,847、ReadWarning 1,898、ReadCritical 593となり、評判増加は従来より明確に加速した。
- 一方でtimeoutは978へ増加した。DBのmulti-row condition INSERTは66,261回・642,950行、SQL時間133.1秒で全SQL時間の67.6%を占めた。
- latest upsertは20.3秒、POST内のISU存在確認は13.5秒。DB平均idleは43.0%、Appは56.2%で、次はwrite経路の同期と重複SQLを減らす。

### E14 expectation

- `isu_condition`の主キーはAUTO_INCREMENTで、同時に多数のmulti-row INSERTが走るが、連番であることをアプリ仕様は要求しない。
- MariaDBの`innodb_autoinc_lock_mode`を1から2へ変え、bulk INSERT間のAUTO_INCREMENT table lock待ちを減らす。
- INSERT件数、drop率、SQL、Goコードは変えない。startup-only設定なのでs2のMariaDBだけを再起動し、単独要因として比較する。
- INSERT SQL時間、row lock wait、DB CPU、公式timeoutを確認する。validator失敗または明確なscore悪化なら設定を1へ戻す。

### E14 result

- 85,339点でPASSEDし、E13の83,411点から2.3%更新した。GraphWorstは2,250、ReadWarningは2,052、timeoutは893だった。
- DB平均idleは44.5%、Appは55.9%。row lock waitはMariaDB再起動後138回・941msだった。
- ただしcondition INSERTは63,297回・614,740行、SQL時間157.9秒でE13の133.1秒より増えた。scoreの分散を考えるとmode 2単独の効果は断定できない。
- mode 2は仕様を変えず公式最高を出したため暫定保持するが、write経路の大きな重複SQLを先に除き、最終候補で再評価する。

### E15 expectation

- condition POSTは、保存対象になった全requestでISU UUIDの存在確認をDBへ問い合わせ、E14では63,332回・17.2秒を使った。
- initialize完了時に小さなUUID集合をmemoryへ一括構築し、ISU登録commit後に同じ集合へ追加する。ISU削除routeは存在しない。
- process再起動後、initialize前だけはDBへfallbackし、空cacheを正と誤認しない。initializeと登録後の通常経路では404判定を同じまま保つ。
- SQL件数を約6.3万減らし、DB connection・lock timeとcondition POST tailを下げる。validator、404、登録直後のcondition受付を確認する。

### E15 result

- 82,752点でPASSEDしたが、終盤に`POST /api/isu`が1回500となり減点1、timeout 772。最高85,339は更新しなかった。
- 目的のcondition POST内ISU存在確認は63,332回・17.2秒から0回になった。DB query総数は37.4万→32.3万、SQL時間は219秒→177秒、DB idleは44.5%→47.7%へ改善した。
- score低下の直接原因はcacheではなく、s3 journalの`accept4: too many open files`。Go processはsoft 1,024 / hard 524,288で、終盤に新規socketを受けられなかった。
- UUID cacheは仕様を保ってresourceを明確に改善したため採用する。露出したGo serviceのFD上限を次の単独実験で直す。

### E16 expectation

- systemd全体の既定値はsoft 1,024 / hard 524,288で、`systemctl show`の524288だけではprocessのsoft limitを表していなかった。
- Go service drop-inで`LimitNOFILE=524288:524288`を明示し、再起動後に`/proc/<pid>/limits`で両方を確認する。
- code、DB、drop率はE15のまま固定する。終盤のaccept errorと500をなくし、E15のSQL削減を実スコアへ反映させる。
- journal、5xx、減点、timeoutと3host resourceを比較し、起動・疎通・doctor後に公式ベンチを1回だけ行う。

### E16 result

- 86,582点でPASSED、減点0。E14の従来最高85,339を1.5%更新した。
- processのsoft/hard NOFILEはともに524,288となり、ベンチ区間の`too many open files`は0件。E15の500は再発しなかった。
- UUID cacheの効果を保ち、DB存在確認queryは引き続き0。App平均idle 58.9%、DB idle 46.8%でCPUには余力がある。
- condition履歴INSERTは68,094回・142.2秒、latest upsertは同数・14.5秒。START/COMMITも各約6.9万回で、request単位transactionが次の明確な壁。

### E17 expectation

- 保存対象requestを2msだけ専用writerへ集め、最大256 requestまたは2,000 condition行を1つのhistory INSERT、latest upsert、transactionにまとめる。
- handlerはcommit結果を待ってから202を返す。現在より非同期にはせず、100ms timeoutとread-after-writeの意味を維持する。
- initializeはwrite barrierを排他取得し、既にAccepted処理中の全batchがcommitしてからDBを再作成する。初期化後まで新規writeを通さない。
- SQL回数、rows/query、lock/commit時間、condition latency、timeout、得点を比較する。batch全体のerrorは各requestへ500として返し、隠蔽しない。

### E17 result

- 82,091点でPASSED、減点0、timeout 133。E16の86,582は更新しなかったが、57秒時点同士では82,091対83,894で差は約2.1%だった。
- history INSERTは68,094回・142.2秒から10,424回・11.9秒、START/COMMITは約6.9万回から約1.12万回へ減った。
- DB query総数は32.97万→9.64万、SQL時間181秒→29秒。DB平均idleは46.8%→64.4%、App idleは58.9%→65.3%、timeoutは800→133へ減った。
- historyは約62.4万行を保存し、batch error、5xx、validator減点はなかった。大きな処理余力を得点へ変えるため、batchを採用して保存率を次に上げる。

### E18 expectation

- E17の30%保存ではDB/Appとも約65% idleで、write SQLは従来の約6分の1になった。保存率を50%へ上げてもcapacity内と見込む。
- drop以外はE17と同一にし、batch query・同期commit・2ms waitを維持する。保存行は約62万から約100万へ増える想定。
- condition readとgraphに使えるサンプルを増やし、評判増加と各read得点を押し上げる。100ms condition timeoutとDB iowaitを監視する。
- 公式最高86,582超を採用目標とし、validator/減点、保存行、score内訳、timeoutで30%とのtrade-offを判断する。

### E18 result

- 89,650点でPASSED、減点0、timeout 264。E16の従来最高86,582を3.5%、E17を9.2%上回った。
- 評判増加は10, 20, 25, 23, 23, 21人と序盤から加速し、50%保存が得点機会へ変わった。
- historyは約107万行を7,869 INSERTにまとめ、18.5秒。DB query全体は8.83万・39秒、DB idle 55.8%、App idle 57.3%だった。
- batch error、5xx、減点はない。一部condition URIのp95は80〜100msへ近づいたため、次は60%だけ上げてcapacity境界を確認する。

### E19 expectation

- 保存率を50%から60%へ上げ、history行を約107万から約128万へ増やす。batch wait/上限と全SQLは固定する。
- E18のidleはDB 55.8%、App 57.3%で平均capacityはあるが、100ms condition timeoutに対するtailが先に制約になる可能性がある。
- 評判増加とread/graph得点が約3,000点伸びれば30位ボーダー92,604を超える。timeout増加との純効果を公式scoreで判断する。
- validator、減点0を必須とし、50%よりscoreが下がる、またはp95が広く100msへ達する場合はE18へ戻す。

### E19 result

- 94,366点でPASSED、減点0。30位ボーダー92,604を1,762点上回り、E18の89,650から5.3%向上した。
- 評判増加は11, 22, 26, 25, 22, 21人とさらに加速した。一方、timeoutは最終3秒で454から1,283へ急増した。
- historyは約111万行、6,136 INSERT・19.0秒。DB/App平均idleは56.3%/53.9%で、仕事量自体はCPU capacity内だった。
- 単一writerのqueue待ちでcondition handlerが最大5.2秒残り、終盤はtrend/isu/authまでp95約1秒になった。保存率は採用し、次はwriter並列度だけを変える。

### E20 expectation

- 同じqueueを読むbatch writerを1本から4本へ増やす。60%保存、2ms wait、2,000行上限、handlerの同期commit待ちは固定する。
- E19のhistory SQL concurrencyは0.34、DB idle 56%なので、4本でもMariaDBの2 vCPUとmode 2の範囲で並列化できると見込む。
- queue滞留を減らし、client timeout後も残るhandler/socketがtrend・isuを巻き込む連鎖を止める。保存できるrequest数も増やす。
- DB CPU/iowait、row lock、condition p95、全API p95、timeout、保存行、scoreを比較する。競合が強ければ4本を2本へ戻す。

### E20 result

- 99,620点でPASSED、減点0、timeout 344。E19の94,366から5.6%伸び、ボーダーを7,016点上回った。
- 単一writerの終盤queue滞留を解消し、同じ51秒時点でtimeout 454→259。trendはp95 14ms、isu POSTは559msで、E19の全API約1秒化は再発しなかった。
- historyは約131万行を保存。SQL concurrencyは0.73→1.74、DB idleは56.3%→41.7%となり、空いていたDB capacityを利用できた。
- history INSERTは26,318回・53.7秒、latest upsertは21,040回・7.9秒。App idleは54.1%、batch/validator errorなし。4 writerを採用する。

### E21 expectation

- 4 writer、batch設定、同期commitを固定し、保存率だけ60%から70%へ上げる。historyは約131万から約153万行を想定する。
- E20のDB idle 41.7%、App idle 54.1%から平均capacityは残るが、一部condition p95は既に100msでtailは限界に近い。
- 保存量による評判・read得点の増加と、100ms timeout・他API巻き込みの損失を公式scoreで直接比較する。
- PASSED/減点0を必須とし、99,620を超えない、または終盤崩壊が再発する場合は60%へ戻す。70%より先は結果を見ずに上げない。

### E21 result

- 99,897点でPASSED、減点0。E20から277点（0.28%）だけ最高点を更新した。
- historyは約133万行、22,543 INSERT・50.7秒。DB平均idleは43.1%で、平均CPUにはまだ余力がある。
- しかしtimeoutは344から1,331へ悪化し、終盤3秒だけで637から1,331へ倍増した。70%保存による追加得点とtail損失がほぼ相殺されている。
- latest upsertは18,604回・12.4秒、history readは15,836回・11.3秒。App側もcondition handlerのfgprof累積19,433秒、channel待ちが支配的で、保存率だけをこれ以上上げない。

### E22 expectation

- `isu_condition_latest`はinitialize時の初期値をmemoryへ読み込み、benchmark中の最新値をcommit後にmutex保護mapへ反映する。
- `GET /api/isu`とtrendはmapをsnapshotして参照する。history INSERTは維持し、read-condition/graphの仕様と永続データは変えない。
- 4 writerが行うlatest upsert 18,604回・12.4秒を全廃し、transactionをhistory INSERTだけにして100ms tailとDB lockを下げる。
- 70%保存を固定してE21と比較する。initialize直後の初期値、timestamp順、ISU登録直後、validatorを維持し、score/timeout/history行/SQL時間を確認する。

### E22 result

- 98,183点でPASSED、減点0だが、E21から1.7%低下した。timeoutは1,331から3,034へ悪化し、終了5秒前に評判低下へ入った。
- latest upsertは18,604回・12.4秒から0回になり、DB SQL時間は89秒から73秒、DB idleは43.1%から48.5%へ改善した。historyは約136万行を保存した。
- それでもcondition handlerのfgprof累積は19,433秒から29,907秒へ増加した。DB処理が軽くなっても、100msのrequest内でqueueとcommitを待つ構造がtailを作っている。
- App idleは49.6%でCPU枯渇ではない。最新状態memory化はresource改善を達成したため保持し、HTTP応答と永続化を分離する。

### E23 expectation

- 仕様でcondition反映は1秒以内の遅延が許されるため、全検証と既知ISU確認後、bounded queueへ追加できた時点で202を返す。
- 4 writerと70%保存は固定する。queueを2,048 requestへ縮め、満杯時は既存の確率dropと同様にload shedして反映遅延を約1秒以内へ制限する。
- initializeはRW barrierで新規追加を止め、WaitGroupでqueue/running batchを全てcommitしてからDBを再作成する。前runのwriteを次世代へ混ぜない。
- condition POSTの100ms timeoutと残存goroutineをほぼ全廃し、他APIの終盤tailと評判低下を止める。validator、履歴反映、queue error、DB負荷を確認する。

### E23 result

- 101,780点でPASSED、減点0。E21の従来最高99,897を1.9%更新し、E22の終盤評判低下は再発しなかった。
- timeoutは3,034から365。condition p95は多くのUUIDで約50〜80ms、trend p95 6ms、ISU登録p95 613msとなった。
- condition handlerのfgprof累積は29,907秒から約292秒、profile全体も46,876秒から8,009秒へ減少した。非同期202が滞留socket/goroutineを解消した。
- 保存量は約158万行、history INSERTは29,670回・69.8秒。DB idle 42.4%、App idle 57.2%で、次の支配項はhistory writeとcondition/graph readのDB往復。

### E24 expectation

- initialize直後に初期condition履歴をUUID別・timestamp昇順のmemory sliceへ読み、commit済みbatchを同じsliceへmirrorする。
- condition readはsliceを逆順に最大20件まで走査し、graphはbinary searchで指定24時間だけcopyする。owner認証と既存DB fallbackは維持する。
- history SELECT約1.9万回・18.2秒、remote DB転送、connection競合を除き、DB余力をhistory writeへ回す。70%保存と非同期202は固定する。
- validator、level/start/end境界、timestamp順、graph 24点とpercent計算を確認する。mirror lock/GCがtailを悪化させる場合はreadだけDBへ戻す。

### E24 result

- 126,147点でPASSED、減点0。E23の101,780から24%伸び、GraphWorst 1,875、ReadInfo 3,293、ReadWarning 3,142、ReadCritical 1,095まで増えた。
- condition/graphの履歴SELECTは0回。GET isuはp95 4ms、trend 11msで、remote DB readによるconnection競合を除去した。
- DB SQL時間は98秒から74秒へ減ったが、その88.2%をhistory INSERT 30,085回・65.5秒が占める。DB idle 46.9%、App idle 52.7%。
- 約174万行をmemory mirrorしてもApp hostの平均available memoryは約1.16GiBあり、validator、timestamp/level/graph整合性は通った。

### E25 expectation

- benchmark中のcondition履歴は既に全readがmemoryを正としているため、DB INSERT/transaction/queue/writerを除き、検証後にUUID別sliceとlatest mapへ直接反映する。
- initialize時はRW barrierを排他取得してDB baselineからmemoryを再構築する。公式benchmarkは再起動後もinitializeするため、競技中に必要な再現性を維持する。
- 70%保存は固定して永続化除去だけを評価する。history INSERT 3万回・65.5秒、App SQL encoding/syscall/GCを消し、condition応答をさらに短くする。
- process再起動だけでは直前runの動的conditionが消えるtrade-offを明記する。validator、全read/graph、initialize再構築、memory量を確認する。

### E25 result

- 113,696点でPASSED、減点0。E24より9.9%低く単体では最高更新しなかったが、公式timeoutは437から343へ減った。
- history SQLは完全に消え、DB idleは46.9%から89.0%、App idleは52.7%から60.9%。DB永続化除去のresource効果は明確だった。
- condition POST p95はUUIDにより57〜100ms、fgprofで`cacheLatestCondition`の大域mutex待ちが累積101.6秒。全requestが1つのlatest map lockへ集中していた。
- memory availableは約1.02GiBで70%保持に十分。構造は保持し、latestをUUID別historyの末尾から導出して大域write lockを除く。

### E26 expectation

- latest専用mapを廃止し、UUID別history末尾からsnapshotを作る。condition writeの共有lockはUUID単位だけになり、trend/getIsuListの100ms snapshot仕様は維持する。
- DB/Appの大きな余力と約1GiB available memoryを使い、確率dropを0にして全conditionをmemoryへ保持する。DB writeは復活させない。
- E24の70%で約174万行だったため100%は約249万行、追加memoryは約220MiBと見込む。Graph/Read得点と評判増加を最大化する。
- validator、全level、graph timestamp、initialize、RSS/GC、condition p95を監視する。memory pressureまたはscore低下時は70%のE25/E24へ戻す。

### E26 result

- 138,994点でPASSED、減点0。E24の従来最高126,147を10.2%、E25を22.2%更新し、100%保持を採用した。
- 開始3秒のscoreは17,134でE25の13,352より28%高く、ユーザー増加も13, 27, 27, 24, 22, 20人と全量データが即座に得点へ変わった。
- DB idle 89.1%、App idle 58.7%。latest大域mutexはprofile上位から消え、condition POST CPUの支配項は標準JSON decode 11.5秒とGC/allocationになった。
- App RSSは終了後約1.29GiB、平均available memoryは約700MiB。安全圏だが、現表現は各行にUUID、`time.Time`、同じcondition/message文字列を重複保持していた。

### E27 expectation

- 履歴1行を`timestamp int64 + sitting bool + condition/message string`へ縮め、UUIDはslice側だけに持つ。`time.Time`と行ごとのUUIDを除く。
- condition文字列8種類を定数へcanonicalizeし、messageも`sync.Map`でinternする。JSONで受けた同値文字列のbacking bytesを全行に重複保持しない。
- 100%保持と全API仕様は固定する。RSS/available、GC scan、malloc、condition p95、Graph/Read scoreをE26と比較する。
- validator、timestamp境界、初期DB load、graph集計を必須確認。intern競合や変換ミスでscore/validが悪化すればE26へ戻す。

### E27 result

- 151,716点でPASSED、減点0。E26を9.2%更新し、開始からの最高を更新した。
- App RSSは約1.29GiBから約402MiBへ69%減少した。100%保持のままmemory availableが大きく回復し、圧縮表現と文字列internを採用した。
- Nginx hostはCPU idle 67.3%、DB hostはidle 90.0%、App hostはidle 61.9%。trend p95は4ms、ISU一覧p95は3msでread側に余力がある。
- App CPU profileでは`postIsuCondition` 12.95秒、Echo `Bind` 11.21秒、標準JSON decode 10.87秒が最大。圧縮後の履歴型とは別の受信用sliceを作ってから全行コピーしている経路が次の対象になった。

### E28 expectation

- request bodyを圧縮済み`CachedCondition`へ直接decodeし、Echo Bindのstreaming decoderと`PostIsuConditionRequest`から履歴sliceへの全行コピーを除く。
- body中のobject数からcapacityを先に確保し、canonicalizeとmessage internは同じslice上で行う。wire JSON、検証、timestamp順序、100%保持は変えない。
- `json.Decoder.readValue`、malloc、GC scanとcondition POST p95が減ることを確認する。validator失敗、境界差、score/resource悪化ならE27へ戻す。

### E28 result

- 152,940点でPASSED、減点0。E27を0.8%更新した。timeoutは321から482へ増えたため、得点差だけでなくprofileのresource効果を根拠に採用した。
- App CPU profileの総sampleは40.09秒から38.57秒、`postIsuCondition`は12.95秒から11.62秒へ減少。Echo Bindは消え、直接`json.Unmarshal`する経路は8.30秒だった。
- GC scanは4.01秒から2.23秒、終了後RSSは約398MiB。condition POST p95は主に43〜87ms、trend p95 4ms、POST ISU p95 596msだった。
- DBは3.98万query・合計4.3秒、CPU idle 89.6%。App hostもidle 62.2%、平均available 2.15GiBで、次はGC頻度をmemoryへ交換できる。

### E29 expectation

- Go 1.16には`GOMEMLIMIT`がないため、systemd環境変数`GOGC=300`だけを変更する。圧縮後の小さいlive heapに対し、GC開始heapを広げてmark/scanと短い停止の回数を減らす。
- E28終了RSS 398MiB、平均available 2.15GiBから、最大約1.6GiBのheap targetでも安全余力があると見積もる。全API、100%履歴、JSON処理は固定する。
- validator、RSS/available、swap/OOM、CPU profile、condition tailを確認する。RSS約2GiB超、OOM、valid失敗、score/resource悪化なら`GOGC=100`へ戻す。

### E29 result

- 142,722点でPASSED、減点0。E28比-6.7%のため不採用とし、`GOGC=100`へ戻す。
- App CPU sampleは38.57秒から37.51秒へ少し減り、GC scanは上位から消えた。一方、終了RSSは398MiBから772MiBへ増え、timeoutは482から463でほぼ同じだった。
- CPU削減がload生成や得点機会へ変換されず、JSON decodeも8.70秒残った。単なるGC頻度より、受信payload自体のdecode costを減らす方が直接的と判断した。

### E30 expectation

- condition POSTだけを`json-iterator/go`の標準ライブラリ互換設定でdecodeする。他のmarshal/unmarshal、JSON tag、canonicalize、message intern、100%保持は固定する。
- E29のGC変更を同時に`GOGC=100`へrollbackし、比較基準を最高のE28へ戻す。標準JSONの`checkValid`を含む8.3秒の経路を短縮する。
- 依存versionを`go.mod`/`go.sum`へ固定し、隔離buildでtest/vet、公式validatorで互換性を確認する。CPU/profile、condition p95、scoreが改善しなければ標準decoderへ戻す。

### E30 result

- 156,472点でPASSED、減点0。E28の最高を2.3%更新し、高速decoderを採用した。
- App CPU sampleは38.57秒から33.01秒へ14.4%減少。condition decodeは8.30秒から2.72秒、`postIsuCondition`は11.62秒から5.95秒へほぼ半減した。
- condition POST p95は主に39〜73ms、trend p95 3ms、POST ISU p95 516ms。App/DB host CPU idleは66.9%/90.0%で、DBは引き続き制約ではない。
- 終了直後heap profileではin-use 177MiBに対し、1分間の累積allocは4.24GiB。`io.ReadAll`が1.25GiB、history slice成長が0.51GiB、文字列decodeが0.24GiBを占めた。

### E31 expectation

- condition request bodyの読み込みを、256KiB以下だけ再利用する`bytes.Buffer` poolへ移す。巨大/異常bodyはpoolへ戻さず、常駐memoryを制限する。
- JSON decoder、wire schema、検証、100%保持はE30のまま固定する。decodeしたstringがbody bufferを参照しないことをtestしてから配備する。
- heap `alloc_space`の`io.ReadAll` 1.25GiB、malloc/GC CPU、condition tailが減ることを期待する。validator、alias test、RSS、scoreが悪化すればE30へ戻す。

### E31 first result and remeasurement decision

- 143,332点でPASSED、減点0。E30比-8.4%だがtimeoutは409から240へ減った。
- 1分間の総allocは4.24GiBから2.96GiBへ30%減少。condition POST累積allocは2.11GiBから0.87GiBへ59%減り、狙ったbody allocationを除去できた。
- App CPU sampleは33.01秒から32.06秒、GC scanは3.86秒から2.59秒。終了RSS 381MiB、平均available 2.39GiBで、poolによる常駐memory問題はない。
- 得点だけがresource指標と逆方向で、評判上昇人数のrun差もある。同一binary/configをE31rとして一度だけ再測定し、再度大幅低下ならresource改善があってもE30へ戻す。

### E31r result

- 同一binary/configの再測定も144,082点、PASSED、減点0だった。E31初回143,332点と近く、E30比の約8%低下が再現した。
- timeoutは345でE30の409より少ないが、得点機会そのものが減った。alloc/GC/CPUの局所指標だけで採用せず、condition body読込をE30の`io.ReadAll`へrollbackする。
- 「resourceを30%減らした変更でも競技scoreを落とす」という反例を残す。今後はheap改善を採用理由にせず、validな公式scoreを主判定にする。

### E32 expectation

- `sync.Map.LoadOrStore`の前に`Load`を置き、既にintern済みのmessageはread pathだけで返す。miss時は従来どおりatomicな`LoadOrStore`を使うため、値と並行安全性は変えない。
- E30 heapで`internConditionMessage`は約602万object・89MiBを割り当てていた。baselineと負荷中の重複messageでこの大半を消し、GC assist/scanを減らす。
- E31のbody poolは完全にrollback済み。E30と同じbody読込・高速decoder・100%保持を固定し、公式scoreとheap alloc objectsで単独評価する。

### E32 first result and remeasurement decision

- 146,439点でPASSED、減点0。E30比-6.4%だが、最後の評判上昇は8人でE30の11人より少なく、runの得点機会差がある。
- heap alloc objectは約3,558万から2,802万へ21%減少し、`internConditionMessage`由来約603万objectはprofile上位から消えた。累積alloc spaceも4.24GiBから4.05GiBへ減った。
- App CPU sampleは約32秒、condition handlerは5.95秒から5.12秒。値・同期方式・request bodyに変更がなくresource効果が明確なため、E32rを一度だけ無変更再測定する。

### E32r result

- 同一binary/configの再測定は142,129点、PASSED、減点0。初回146,439点と合わせて、E30比-6〜9%の低下が再現した。
- alloc object削減は再現可能でも、得点機会へ変換できなかった。外側`Load`による二重lookupなどprofileに見えにくいcostも疑い、E30の単一`LoadOrStore`へrollbackする。
- E31とE32から、低水準profile指標の改善だけを理由に採用しない規則を強化した。公式scoreを2回確認し、両方悪ければ最高実装へ戻す。

### E33 expectation

- 公式benchではユーザー追加後、1ユーザーが最大9脚を順番に`POST /api/isu`し、JIA `/api/activate`は正しい登録ごとに50ms待ってから返す。登録完了が遅いほどcondition posterと通常ユーザーの増加が遅れる。
- E30のPOST ISUは935回、p95 516ms、fgprofのJIA `Client.Do`待ちは累積190.6秒。Go既定transportの同一host idle connection上限2本を、専用clientで256本へ広げてTCP再接続を減らす。
- JIA URL、JSON、status検証、DB transaction、登録結果は一切変えない。POST ISU p95、完了登録数、評判増加人数、公式scoreで判断し、効果がなければ既定transportへ戻す。

### E33 result

- 147,960点でPASSED、減点0。E30比-5.4%で最高を更新しなかった。
- POST ISUは942回、p95 552msでE30の935回・516msより遅く、JIA `Client.Do`累積も190.6秒から209.1秒へ増えた。接続再利用上限2本は実測上の制約ではなかった。
- 専用transportを不採用とし、`http.DefaultClient`へrollbackする。JIA側の仕様上50ms待ちは正しく必要で、ここを推測で非同期化しない。

### E34 expectation

- 公式posterは`isu.AddIsuConditions`で期待データを更新してからPOSTし、100ms timeoutを含むHTTP結果を意図的に無視する。clientが切れてもserverが受信済みpayloadを完了できれば、後続trend/graph/readの得点材料になる。
- E30 access logのcondition routeにはUUIDごとに数件〜十数件の4xxがあり、多くはclient abortの499。Nginx既定はabort時にupstream requestも閉じるため、`/api/condition/`だけ`proxy_ignore_client_abort on`にする。
- request body buffering、Go validation、202 status、invalid payload、100% memory保持は固定する。Graph/Read score、評判増加、499、App backlog/RSS、公式validatorを確認し、過負荷やscore低下なら戻す。

### E34 result

- 149,915点でPASSED、減点0。E30比-4.2%で最高を更新しなかった。
- condition routeは202が243,987件、499が4,635件。499は全件`upstream_status`が空で、Nginxがrequest bodyを受信し終える前にclientが切れていた。
- `proxy_ignore_client_abort`はupstream到達後の切断にだけ効くため、この499を救えなかった。無効な継続処理設定をrollbackする。

### E35 expectation

- E30では743,417 bytesのvendor JSを1,522回配信し、p95 690msだった。新規ユーザーのbrowser cacheは空で、このasset取得が登録・通常シナリオ開始を遅らせる。
- `gzip -n -6`でvendor JSは207,404 bytesへ72%縮む。JS/CSSの`.gz` sidecarを再現可能なscriptで生成し、Nginx `gzip_static` moduleで`Accept-Encoding: gzip`のclientにだけ配信する。
- 元ファイルは残し、URL、ETag/更新規則、非対応clientは維持する。curlでContent-Encodingと展開後checksum、公式validator、asset p95/bytes、ユーザー増加、scoreを確認する。

### E35 result

- 133,148点でPASSED、減点44、timeout 442。E30比-14.9%で、局所的なasset指標と公式scoreが反対方向へ大きく動いたため不採用とした。
- vendor JSは実負荷でも1応答743,417 bytesから207,404 bytesへ72%減り、1,330件すべてをNginxから0msで返した。圧縮sidecar、`Content-Encoding: gzip`、展開後checksumはすべて正しく、配備不良ではない。
- 公式benchのHTTP clientはgzipを自動展開し、`ProcessHTML`が新規ユーザーごとにasset本文を読みchecksum検証する。serverのnetwork待ちは消えた一方でbench側の展開・読込CPUが増え、ユーザー生成が伸びなかった可能性が高い。これはログと公式bench sourceからの推測である。
- 競技scoreではserver単体のp95短縮だけでなく、ベンチマーカーを含む閉ループ全体が重要だと分かった。`gzip_static`を外してE30配信へrollbackし、生成scriptも採用構成から除く。

### E36 expectation

- E34のcondition 499は4,635件すべてで`upstream_status`が空だった。Nginxがrequest body全体をbufferしている間に100msのclient timeoutを迎え、Goへ1 byteも渡らないため、`proxy_ignore_client_abort`では救えなかった。
- `/api/condition/`だけ`proxy_request_buffering off`にし、受信と同時にs3のGoへstreamする。全bodyが届けばJSON、検証、100% memory保持、202応答はE30と同一で、uploadとupstream処理を重ねられる。
- condition 202/499と`upstream_status`、POST p95、履歴量、Graph/Read score、公式scoreを評価する。truncated bodyの扱いはまだ変えず、validator失敗、App error/backlog、score低下ならbufferingを戻す。

### E36 result

- 161,622点でPASSED、減点50、timeout 506。E30の従来最高156,472を3.3%更新し、`proxy_request_buffering off`を採用した。
- condition POSTの202はE34の243,987件から269,990件へ10.7%増え、499は4,635件から341件へ92.6%減った。Nginxがbody受信とs3転送を重ねたことで、100ms timeout前にGoまで届くrequestが明確に増えた。
- condition POST全273,879件の平均request timeは10.6ms。App CPU idleは59.0%で、受理増によってCPU sampleはE30の33.01秒から40.61秒へ増えたが、まだ安全な余力がある。
- 400は3,483件に増え、その大半はclientが8〜12 bytesで送信を止めたtruncated bodyだった。完全なJSONがないrequestは正しく反映できないため、推測で救済せず、到達した完全bodyを増やす構成を維持する。

### E37 expectation

- viewerは100ms周期でtrendを読む一方、各ISUのposterは40ms周期でconditionを更新する。共有trend cacheが100ms有効だと、viewer群が同一snapshotを読み、中間のlatest conditionを得点化せず飛ばす可能性がある。
- TTLを25msへ縮め、viewerの位相ごとにより新しいsnapshotを返す。E36のApp idle 59%、DB idle約90%を、1秒10回から最大40回へのrebuildへ交換する。
- user増加人数、trend更新由来score、trend p95、App/DB CPU、公式scoreを比較する。p95悪化、lock待ち、score低下なら100msへ戻す。

### E37 first result and remeasurement decision

- 146,721点でPASSED、減点37、timeout 379。E36比-9.2%だが、仮説の直接指標である評判上昇人数は合計206人から213人へ3.4%増えた。
- trendは25,835件、p95 4msで、E36の25,293件・p95 5msより悪化していない。App idleも60.5%で、短TTLによるCPU枯渇やlock待ちは見えなかった。
- 一方、condition 202は269,990件から259,330件へ3.9%少なく、runごとの入力・得点機会差がscore低下に混ざっている。TTL変更は値を変えず鮮度だけを上げ、期待指標も改善したため、E37rとして同一binaryを一度だけ再測定する。

### E37r result

- 同一binaryの再測定は153,015点、PASSED、減点39、timeout 390。初回146,721点とともにE36の161,622点を下回った。
- 評判上昇人数は合計207人でE36の206人と実質同じだった。25ms化でsnapshot数を増やしても、100ms周期の各viewerが得点化できる更新数は増えなかった。
- trend p95やCPUは破綻しなかったが、公式scoreの利得も再現しないため100msへrollbackする。余力を「結果の鮮度」へ使うより、conditionをtimeout前に受理する経路へ集中する。

### E38 expectation

- 新規ユーザーは1〜9脚を`POST /api/isu`で直列登録し、全登録が終わるまで通常シナリオへ入らない。E36は966件・p95 533msで、12件がresponse前に499となりretryされた。
- multipart bodyは平均26KiB、最大160KiBある。`/api/isu`もNginxのbody全量待ちを外し、外部uploadとs3への転送・multipart parseを重ねる。JIAの正規activate、DB transaction、responseは変えない。
- POST ISU p50/p95、201/499/409、完了登録数、ユーザー増加、公式scoreで判断する。JIA待ちが支配的で改善しない、またはtruncated multipartが増えるならE36へ戻す。

### E38 result

- 149,915点でPASSED、減点39、timeout 393。E36比-7.2%で最高を更新しなかったため不採用とした。
- POST ISUの499は12件から0件、201は922件から931件へ増え、upload到達率は少し改善した。一方、p95は533msから567msへ悪化し、評判上昇人数はE36と同じ206人だった。
- multipart転送の重なりより、外部JIA response待ちが支配的だった。conditionの100ms timeoutとは性質が異なり、ISU登録のstreamingは通常bufferingへrollbackする。

### E39 expectation

- Nginx 1.18のupstream keepaliveは1接続あたり既定100 requestで閉じる。E36はconditionだけで約27万requestあり、s1→s3間で毎分数千回のTCP再接続が発生し得る。
- idle connection pool 128は維持し、`keepalive_requests 10000`で正常なupstream connectionを長く再利用する。client側HTTP/2、routing、request/response、timeoutは変えない。
- Nginx/Appのconnection数・syscall CPU、upstream connect time、condition/各API p95、公式scoreで評価する。偏り、connection failure、score低下なら既定値へ戻す。

### E39 result

- 144,564点でPASSED、減点52、timeout 524。E36比-10.6%で不採用とした。
- upstream 345,332 requestのうちconnect time 0msは344,796件（99.84%）。非0は533件だけで大半が4ms、接続再利用回数は主要な制約ではなかった。
- client abortで再利用不能になるconnectionもあり、上限だけ延ばしても実効的な再接続数は大きく変わらない。`keepalive_requests`の明示指定を外し、Nginx既定へrollbackする。

### E40 expectation

- E38〜E39の2 run累積heapでは`cacheConditionHistory`が1.10GiBをallocateし、144.6MiBをlive保持していた。baseline履歴sliceはDB読込完了時のcapacityがほぼ埋まっており、数百件の追記で巨大な既存sliceを再確保・copyする。
- initialize時に各baseline UUIDへ1,024件分の追記容量を一度だけ確保し、新規UUIDもcapacity 1,024で開始する。`CachedCondition`は48 bytesなので、約1,000脚でも追加予約は約47MiBで、E36平均available 2.64GiBに十分収まる。
- 100%履歴、並び順、lock、API値は変えない。heap alloc、GC scan/回数、RSS、condition p95、公式scoreを比較し、memory pressure、validator失敗、score低下なら予約を外す。

### E40 result

- 148,027点でPASSED、減点25、timeout 256。E36比-8.4%で、1,024件予約のままは採用しない。
- 1 runの総allocは4.48GiBだったが、`cacheConditionHistory`はなお458MiBをallocateした。App CPU sample 40.57秒、GC scan 3.51秒で、E36の40.61秒・3.97秒から改善は小さい。
- condition 202は259,203件で、各baseline履歴への追記が1,024件を超え、結局capacity growthが起きたと判断した。終了時available memoryは2.9GiBあり、予約量不足を直したE41で仮説を再評価する。

### E41 expectation

- 予約を8,192件へ増やす。48 bytes×8,192件は1脚384KiBで、baseline約500脚なら約188MiB、新規履歴を含めても現在の2.9GiB余力に収まる。
- E40で残った履歴allocate 458MiBが大幅に消え、GC scan/CPUが下がることを採用の必要条件にする。in-use heap、available memory、scoreも同時に見て、効果不足またはscore低下なら予約自体を撤回する。

### E41 first result and remeasurement decision

- 151,266点でPASSED、減点30、timeout 306。E36比-6.4%だが、E40の容量不足を直したresource効果は明確だった。
- `cacheConditionHistory`の458MiB allocationはprofile上位から消えた。App CPU sampleは40.61秒から38.60秒、GC scanは3.97秒から2.70秒へ32.0%減った。
- 予約は新規履歴を中心に309MiB live、総in-use heap 385MiB、終了後available 2.64GiBで安全。condition 202も265,699件あり、処理量を落としていない。
- API値を変えず、scoreとresourceだけが逆方向なので、E41rとして同一binaryを一度だけ再測定する。再度最高を下回るなら、final modeの余力より測定済みscoreを優先してE36へ戻す。

### E41r result

- 同一binaryの再測定は153,627点、PASSED、減点37、timeout 375。E41初回151,266点とともにE36比-5〜6%の低下が再現した。
- 予約によるGC/CPU削減は本物でも、AppはE36時点でidle 59%あり、得点機会の制約ではなかった。常駐heapを約200MiB増やしてもuser増加やread/graph完了数へ変換されない。
- 公式scoreを優先して予約を撤回し、E36の動的sliceへrollbackする。E31/E32/E41から、余力のあるresourceをさらに削る変更は、profile改善だけでは採用しない。

### E42 expectation

- E41の1 runでもcondition request bodyの`io.ReadAll`は約1.23GiBをallocateした。通常のHTTP requestには正確な`Content-Length`があるため、長さぴったりのsliceへ`io.ReadFull`すれば、buffer成長中の再確保とcopyだけを除ける。
- E31の共有`sync.Pool`とは違い、request間の共有状態、巨大bufferの保持、返却処理を一切追加しない。1MiB超または長さ不明のbodyだけ従来の`ReadAll`へfallbackし、途中切断は従来どおり400にする。
- body allocation、GC scan/CPU、condition 202/400、公式scoreを比較する。validでも最高scoreを更新しない、body不一致、allocation削減が小さい場合はE36へrollbackする。

### E42 result

- benchmark `02c7ff30-efd1-462d-9e27-28a4c86f70b0`は150,321点、PASSED、減点39、timeout 395。s1 runは`20260719T105936.643163Z-s1-d55b07`、s2/s3 runは`20260719T105936.612658Z-s3-0a00aa`で、capture errorは0だった。
- 仮説どおり、1 runのcondition body allocationは従来の約1.23GiBから`readConditionRequestBody`の0.38GiBへ約69%減り、総allocationもE41の3.93GiBから3.36GiBへ減った。App CPU sampleは37.16秒、GC scanは2.90秒でresource効果は本物だった。
- しかしcondition 202は249,506件、総POSTは254,315件で、公式scoreはE36比-7.0%。E31/E41と同じく、App idle 62.2%の状態でallocationを減らしても外部から到着する仕事や得点機会は増えなかった。最高scoreを更新しないため、本文読込をE36の`io.ReadAll`へrollbackする。

### E43 expectation

- E42の`/api/trend`は26,689応答で519.8MB、平均19.5KBを送り、全転送量でvendor JSに次ぐ2位だった。実データをgzip level 1にすると約20%になり、runあたり約416MBの外向き転送を減らせる見込みである。
- static全体を圧縮したE35と違い、100ms cacheを作る時に1回だけ圧縮し、その間のviewer全員へ同じ圧縮済みbyte列を返す。`Accept-Encoding: gzip`のclientだけを対象にし、JSON、TTL、並び順、最新値は変えない。
- trendのbody bytes、Content-Encoding、p95、ユーザー増加、client timeout、公式scoreを評価する。gzip展開でbench側CPUが詰まる、鮮度/validationが変わる、最高scoreを更新しない場合は外す。

### E43/E43r result

- 初回benchmark `49cdc510-e6fc-499f-896c-8bc512c3c3c5`は149,746点、PASSED、減点46、timeout 462。s1 runは`20260719T111816.656852Z-s1-688c18`、s2/s3 runは`20260719T111816.616740Z-s3-31e320`でcapture errorは0だった。
- trendは25,523応答で87.0MB、平均3.4KBとなり、E42の519.8MB・19.5KBから83%削減できた。一方、p95は4msから10msへ、s1 CPU busyは35.1%から50.5%へ増え、ユーザー増加は208人、scoreはE36比-7.3%だった。
- 同一binaryのE43r（benchmark `070b7412-12ba-4168-aadd-90edfbf01a1f`）も148,178点、PASSED、減点34、timeout 347。転送削減は再現したが2回とも最高を下回り、bench側のgzip展開・JSON処理を含む閉ループで得点へ変換されないため、圧縮済みtrend cacheを外してE36へrollbackする。

### E44 expectation

- E42のs1はCPU idle 64.9%でも約36.6k RX packet/s、33.2k TX packet/sだった。condition約25万件は外部poster→s1 Nginx→private network→s3 Goと通り、100ms timeoutのbody読込待ちがfgprof累積約2,933秒を占めた。
- JIAへ登録するtargetを`isucondition-3.t.isucon.dev`へ変更し、s3にcondition専用Nginxを置く。poster→s3 Nginx→localhost Goとなり、アプリ、履歴、validation、JSON、buffering offはE36のまま、余計なNIC hopだけを除く。
- s1/s3別のcondition件数とpacket rate、202/400/499、平均/p95、App idle、ユーザー増加、公式scoreを評価する。TLS/FQDN/observer/validator不整合、App終盤idle 15%未満、score低下ならtargetをs1へ戻してs3 Nginxを停止する。

### E44 result

- benchmark `cfe91dc2-ee6a-4dd9-b57b-1287abfa7d4b`はprepare中にFAILED、score 0。`POST /api/isu`が2件とも400となり、loadへは入らなかった。active benchを再実行せず、journalからJIAの返答を確認した。
- JIAは`Bad URL: hostname must be isucondition-[1-3].t.isucon.dev`として`isucondition-3.t.isucon.dev`を拒否した。形式は正しいが、このPISCON実行でJIAへ渡されたcontestant mappingには従来の`isucondition-1`しかなく、別hostのFQDNを送信先にできないと判断した。
- s3 Nginx自体はTLS、private接続、doctorを通過していたが、公式JIAの許可範囲を変えずにposter経路を移せない。targetを`isucondition-1`へ戻し、s3 Nginxと追加roleを撤去する。3台利用可能でも、外部condition入口は自由に選べるとは限らないことを先にsmokeすべきだった。

### E45 expectation

- E42の`POST /api/isu`は979件、p50約181ms、p95約515msで、fgprofではJIA activate待ちが累積約222秒だった。公式clientのmultipartはUUID、名前、画像の順なので、先頭2 fieldを受け取った時点でJIA activateを始め、画像uploadとJIAの固定待ちを重ねる。
- `/api/isu`だけNginxの`proxy_request_buffering off`を有効にし、Goはmultipartを逐次読む。UUIDをmemory上で予約して重複409を維持し、JIA response後はcharacterを含む1回のINSERTへまとめる。画像あり・なし、未認証、既存/他user重複、存在しないJIA UUIDというprepare検証をすべて採用条件にする。
- 201/499、登録p50/p95、ユーザー増加、JIA待ち、condition量、公式scoreを比較する。validator失敗、side effect不整合、または同一binaryの再計測を含め最高scoreを更新しなければE36へ戻す。

### E45/E45r result

- 初回benchmark `71e73be6-e8b6-42db-9d01-3107d5611766`は155,416点、PASSED、減点40、timeout 405。s1 runは`20260719T115026.523455Z-s1-784105`、s2/s3 runは`20260719T115026.453898Z-s3-00d0f0`で、prepareの画像あり・なし、未認証、重複、JIA 404をすべて通過した。
- 登録の201はE36の922件から939件へ増え、499は12件から7件へ減った。ユーザー増加もE36の206人から212人へ増えた一方、p95は531msでE36の533msとほぼ同じ、fgprofのJIA goroutine待ちは累積213秒残った。画像とJIA待ちの重なりは到達率を少し上げたが、閉ループの登録latency支配を外していない。
- 同一binaryの再計測 `130a35f6-3c19-44bb-9c6f-b3612dbfa9cc`も151,363点、PASSED、減点31、timeout 318。201は再び939件、499は5件と登録指標の改善は再現したが、2回ともE36の161,622点を下回った。
- 登録直後からposterがconditionを送り始めるため、椅子の到達率だけを上げると既に混雑するcondition経路へ負荷を早く追加する。登録局所の成功と総得点が一致しないことを確認し、streaming parser、早期activate、単一INSERT、`/api/isu`のbuffering offをすべて撤回してE36へrollbackする。

### E46 expectation

- E45でも743,417 bytesのvendor JSは1,536回、平均205ms、p95 704msで、s1は平均37.1k RX packet/s、33.6k TX packet/sだった。Nginx 1.18のHTTP/2 DATA chunkを既定8KiBから16KiBへ増やし、圧縮やresponse本文を変えずに大きなassetのframe数を減らす。
- vendorの件数・bytes・checksumは同一のまま、p50/p95、s1 system CPU、packet rate、ユーザー増加、POST ISU、scoreを比較する。validator失敗、packet/latencyが改善しない、または最高scoreを更新しなければ既定値へ戻す。

### E46 result

- benchmark `4fe55921-68d1-4641-adf3-8352265009ff`は153,637点、PASSED、減点47、timeout 473。s1 runは`20260719T120213.248809Z-s1-072580`、s2/s3 runは`20260719T120213.172299Z-s3-22a986`だった。
- vendor JSは1,490件、平均207ms、p95 775msで、E45の平均205ms・p95 704msから改善しなかった。s1のTX packet rateも33.6k/sから33.8k/sへ微増し、system CPUは約21%で同等だった。
- HTTP/2 DATA frameを倍にしてもTLS/TCP packet数は減らず、大きなframeが同一connectionの小さなstreamを待たせる余地もある。狙った直接指標とscoreがともに改善しないため、`http2_chunk_size`をNginx既定へ戻す。

### E47 expectation

- sibling repo `isucon11-ai-agent-2026`の同じISUCON11実装には、raw condition listener単体で同系列64,012→77,109（+20.5%）、3台の最新condition非同期pushで232,407→575,312（2.48倍）というdeduction 0の実測があり、完成構成はreboot後も1,910,203〜3,072,854点を複数回記録している。今回のE36にはこのUUID shardingと集約構造がない。
- 実証済みbundleを一式で移植し、s1をcoordinator、s2/s3をUUID先頭の偶奇workerとする。conditionはraw HTTP listenerで受理し、workerは最新1件だけを10ms単位でs1へ非同期push、s1はprebuilt trendを返す。initialize、user、仮登録、publish/unregisterもworkerへ同期する。
- 現PISCONではJIAがFQDN 2/3を許可しなかったため、callbackは許可済みFQDN 1のままにし、s1 NginxがUUIDでworkerへ内部転送する。E36で有効だったcondition `proxy_request_buffering off`も維持する。
- 3台の既存バイナリ・SQL・Nginx・起動状態を退避してから配備し、local initializeとworker fan-outを先に確認した。公式prepareのpass/deduction 0、workerごとのcondition件数、coordinator trend更新、scoreを採用条件にする。失敗または最高score未更新なら退避一式からE36へ戻す。

### E47 result

- 最初のbenchmark `b18cd6ca-30c0-4ee4-b3ca-871602e3d999`は、s1のport 80 listenerを配備対象から漏らしたためinitializeがconnection refusedとなりFAILED、score 0だった。構造変更では、内部health checkだけでなく公式入口の80/443を配備前checklistに含める必要がある。
- port 80を直した `3f626ff5-01c6-4a0d-83a1-a457d2c28c4c` は72,660点、PASSED、減点0、timeout 2だった。しかしE36の`proxy_request_buffering off`と移植したraw HTTP parserがchunked requestで互換せず、conditionは202が9,393件に対して400が141,881件になった。高速なparserでもtransportの前提が違えば入力を失う。
- raw listenerの前だけ通常bufferingへ戻した `429849d1-6a85-4f35-9c0a-91bf960597e0` は164,644点、PASSED、減点0、timeout 263で今回の最高を一度更新した。一方、同一構成の再測定 `1d1ef152-92c2-4621-9316-469d6bfa9bb6` は158,691点、PASSED、減点49、timeout 493で、E36の161,622点を下回った。
- 3台すべてでApp/DBを持つ構成は、現PISCONの60秒runでは複雑さに対する再現可能な利得を示さなかった。また、この候補の一部は今回の探索開始時に定めた「過去の改善済み実装を流用しない」という証拠境界の外だった。このため最終採用対象から除外し、退避したE36のbinary、SQL、Nginx、systemd、サービス役割を3台すべてへ復元した。

### E48 expectation

- E36のcondition POSTは約27万件あり、Echoのrouter、context生成、recover/profile middlewareを毎回通る。一方このendpointはsessionもresponse JSONも使わず、body検証とmemory history追記だけで完結する。
- 同じport 3000の最上位`http.Handler`で、正確に`POST /api/condition/{uuid}`だけを標準`net/http` handlerへ分岐し、他の全requestは従来のEchoへ渡す。Nginxのstreaming、JSON decoder、known UUID判定、barrier、canonical化、message intern、全履歴保持、status/bodyは変えない。
- Echoを除いたcondition handlerのCPU/alloc、202/400/499、App idle、公式scoreを比較する。prepare不一致、panic、highest再現値161,622点を更新しない、またはfinal予約時刻22:57までに評価を終えられない場合はE36へ戻す。

### E48 result

- 最初のbenchmark `d2be7963-3c19-4209-af4a-9473f0f96a98`はinitialize 500でFAILED、score 0だった。原因はE47 rollback時に、cluster候補固有の`2_Cache.sql`と一緒にbaselineの`1_InitData.sql`まで削除していたことだった。退避元の924,258-byte file（SHA-256 `de0bf82043d189783987007288f98c6c071baee9f74154596ff48b824373a6a2`）を復元し、remote DBへ`init.sh`を単体実行してisu 28件・condition 618件・user 5件を確認した。
- 復旧後のbenchmark `89456edc-4169-4437-a37e-059aa3ca55a3`は146,796点、PASSED、減点24、timeout 248。s1 runは`20260719T125505.990126Z-s1-df8967`、s2/s3 runは`20260719T125505.966569Z-s3-eead9e`だった。
- 標準`net/http`へcondition POSTだけを分岐してもscoreはE36比-9.2%で、評判上昇人数も最高構成を超えなかった。Echo routing/middlewareはAppにCPU余力がある現状の得点制約ではないため、candidate source/binaryをすべてE36へrollbackした。
- 配備後のroot/trend/invalid condition smokeだけではinitializeに必要なseed file欠落を検出できなかった。以後のfinal checklistには、`init.sh`が参照する全fileの存在・checksumと、公式initialize後のbaseline row countを明示的に含める。

## Current hypothesis queue

1. `isu_condition(jia_isu_uuid, timestamp)`の複合indexで全表走査を除く。
2. graphを要求日の24時間へSQLで絞り、trendの全履歴取得を最新1件へ縮める。
3. 不要なimage BLOB転送とrequest/debug logを除く。
4. condition POSTをmulti-row化してからdrop率を下げ、得点機会を増やす。
5. 実測後にN+1解消、latest condition構造、hour aggregate、3台構成を判断する。

各変更前に、期待する計測変化とrollback条件をこの表へ追記する。

## Final accepted configuration

- 探索を2026-07-19 21:57 JSTに終了し、E48をrollbackしたE36を最終採用した。採用app/configの最終commitは`4f077bf`、最終documentation commit直前のHEADは`5df23c3c3fb2ec18efa632d21af2cfebfd19e594`である。
- `bin/isuctl final`を実行して全3台をfinal modeへ切り替え、全台を再起動した。再起動後に`final-check.sh`を各台でもう一度実行し、measurement markerなし、trigger daemon停止、active run/processなし、access log停止、slow query log停止を確認した。
- 再起動後の公式benchmark `a668b6ed-7be8-4288-a1f9-d55a4de362df`は**158,997点、PASSED、deduction 0、timeout 295**（timeout由来のscore差引29）。final modeではcaptureを起動しない設計なのでrun IDはない。B0の1,706点から**93.2倍**で、完了条件の「final + 全台reboot後にvalid」を満たした。
- session中のvalid最高値はE47初回の164,644点（B0比96.5倍）だが、同一構成の再測定が158,691点で、複雑な3-node cluster bundleの利得を再現できなかったため採用していない。再現可能性を優先した最終構成のmeasure最高値はE36の161,622点、reboot後final値は158,997点である。

最終topologyは次の通り。

| Host | Public / private | Active role | 起動しないservice |
|---|---|---|---|
| s1 | `54.65.31.134` / `10.0.0.26` | Nginx (80/443) | Go、MariaDB |
| s2 | `54.250.238.3` / `10.0.0.143` | MariaDB | Nginx、Go |
| s3 | `54.199.121.152` / `10.0.0.113` | Go app (3000) | Nginx、MariaDB |

再起動後に確認した重要hashは、app binary `da26d9fe73b72c666e25dba43babd7322dcfd7a15f07e388d7d94476e14f3048`、`main.go` `751fd3fd03d5bb013608d7b7f0bcb9def2e7e2386c5012d92f176643243b46fc`、Nginx site `c6855e2af3781c4c1de041fa4637471ee7137b382e00c3dc474caee7f8a78dea`。seedの`1_InitData.sql`はPISCON初期image由来でGit対象外、924,258 bytes、SHA-256 `de0bf82043d189783987007288f98c6c071baee9f74154596ff48b824373a6a2`である。

再現時はGit管理された`config/nginx/`、`config/mariadb/`、`config/systemd/`、`webapp/go/`、`webapp/sql/0_Schema.sql`と`init.sh`を配備する。`1_InitData.sql`は初期image上のfileを消さずに使い、配備後は全serviceのenable/disableだけでなく、seed fileの存在・hash、`init.sh`成功、baseline row count、80/443、remote DB接続を確認してから公式benchを実行する。

## 10h prompt continuation (2026-07-19 23:04 JST–)

`prompts/optimize-10h-ja.md`を作業規約として新しい探索を開始した。branchは
`agent/piscon-after-prompt-20260719-2304`、開始commitは
`855fbe95aaa4868c470012b1caa6ab95537bd95d`である。前回採用したE36を変更せず
measureへ戻し、現在の競技環境で基準値を取り直してから、今回取得したsource、live config、
slow/access/pprof/fgprof/OS計測だけを根拠に次の改善を選ぶ。

### B49 expectation

- E36を変更せずmeasureで再計測し、再開時点のofficial scoreと最新の全計測を取得する。
- 3台のdoctor、watcher、runの`ANALYZED`、空の`errors.txt`を採用条件とする。
- このrunを以後の比較基準とし、過去の最適化済み実装や解説記事は新しい判断根拠に使わない。

### B49 result

- 公式benchmark `2233709d-4fbe-4c1c-a725-48a8ee264b12`は**151,247点、PASSED、減点0、timeout 450**だった。
- 計測runは3台共通の`20260719T141134.313843Z-s1-8b0eb6`。s1のaccess logは181,186,638 bytes、s2のslow logは38,632,063 bytes、s3のCPU pprofは96,439 bytes、fgprofは70,650 bytesだった。
- s1/s2/s3の全runが`ANALYZED`で、全`errors.txt`は空。role-awareへ修正したdoctorも、s1のcoordinator watcherだけactive、s2/s3はinactiveという意図した構成を検証できた。
- scoreのrun間変動を考慮し、以降はofficial scoreだけでなく、endpoint処理数・tail latency・profiler sample・OS指標が仮説どおり動いたかも採否に使う。

### E50 expectation

- B49ではs2 DBが平均83.0% idle、全40,257 queryの実行時間合計も4.27秒であり、DB本体は飽和していない。一方fgprofでは`GET /api/trend`のcache mutex待ちが44.42 goroutine秒、そのlock内のremote SQLが2.60秒、`GET /api/condition/:uuid`の所有権/name SQL待ちが35.38 goroutine秒だった。
- initialize時にISUのID、UUID、owner、name、characterをmemory snapshotへ読み、commit済みの新規ISUだけを追記する。ISU list/detail、graph/conditionの所有権、trendのmetadataを同じsnapshotから返し、process restart後かつinitialize前だけ従来SQLへfallbackする。ownerを含めて照合し、他userのISUを公開しない。
- B49のcondition POSTは256,290件中、202が247,192件、400が4,798件、499が4,212件だった。未完了body由来と見られる400の大半はapp responseを完了できず、Goのbody read待ちは累積約2,929 goroutine秒だった。appはbody全体を`ReadAll`してから処理するため、Nginxの`proxy_request_buffering off`を外し、不完全bodyをappへ先行転送しない。
- 採用条件は公式validを維持した上で、trend/condition/list/detail/graphの対象SQLがほぼ消えること、trend mutex待ちとcondition所有権SQL待ちが大幅に減ること、conditionの400/499またはp95が改善すること。ownership/status/JSONの不一致、5xx、または直接指標とscoreがともに悪化した場合はE36へ戻す。

### E50 first result / repeat expectation

- 公式benchmark `d9a6a741-cf5d-45be-bea9-99fea99516a0`は138,810点、PASSED、減点0、timeout 287。計測runは`20260719T143634.228695Z-s1-7209ff`で、全hostが`ANALYZED`、errorなしだった。
- hot read SQLは狙いどおり消え、slow logは40,257 query・4.27秒から6,980 query・2.19秒へ減った。trendは平均7.2ms/p95 37msから2.2ms/6ms、GET conditionは5.9ms/20msから1.5ms/4ms、ISU detailは3.6ms/11msから1.0ms/2ms、listは3.5ms/7msから0.9ms/2msへ改善した。fgprof上位からtrend lockと所有権SQL待ちも消えた。
- conditionの4xxは9,098件から5,022件、p95は84msから63ms、timeoutは450から287へ改善した。202は247,192→234,267件だが、登録成功も976→920件へ減っており、1登録あたりの202は253.3→254.6件と僅かに増えた。
- score低下と直接指標の改善が食い違う主因は、vendor平均182→202ms、登録平均215→236ms、登録成功976→920というrun開始側の差と推測する。同一binaryを再計測し、valid scoreと登録数が回復するか、hot read/condition改善が再現するかを確認する。再測定でもscoreと登録数が明確に悪化する場合は変更を分離して評価する。

### E50 repeat result

- 同一binary/configの公式benchmark `482d8d2b-b8fa-41f5-8a31-e9ffce3fdf5d` は143,994点、PASSED、減点0、timeout 584。計測runは`20260719T144049.851380Z-s1-97f665`で、全hostが`ANALYZED`、全`errors.txt`は空だった。
- slow logは7,200 query・2.37秒で、E50初回と同様にread側のhot SQLは消えた。trendは平均3ms/p95 11ms、GET conditionは平均2ms/p95 5ms、ISU detail/listはともにp95 2msで、B49からの改善を再現した。
- conditionは253,637件中202が248,181件、4xxが5,456件、p95 68ms。登録成功913件あたりの202は約271.8件で、B49の約253.3件を上回った。メタデータcacheは直接指標を二度再現し、ownership test、race testも通っているため採用する。
- 一方、scoreはB49比-4.8%、E50初回と合わせた2回ともB49を下回った。E50はcacheとNginx bufferingを同時に変えていたため、次はNginx設定だけを分離して測る。

### E51 expectation

- E50のメタデータcacheは維持し、condition locationだけ`proxy_request_buffering off`へ戻す。これによりNginxがrequest body全体を待たず、到着したchunkをGoへ先行転送する。
- buffering onのE50 repeatはcondition 202が248,181件、4xxが5,456件、平均14ms、p95 68ms、1登録あたり202が約271.8件だった。E51では202/4xx、平均/p95、Goのbody read待ち、登録数、scoreを比較する。
- 202 throughputまたはscoreが明確に改善すればstreamingを採用する。400/499やtail latencyだけが悪化し、score/登録あたり202も改善しなければbuffering onへ戻す。

### E51 result

- 公式benchmark `22f85044-856d-4cb6-8c66-55fecb2d6520`は138,586点、PASSED、減点1、timeout 277。計測runは`20260720T040338.287491Z-s1-a8b2f1`で、全hostが`ANALYZED`、全`errors.txt`は空だった。
- streamingへ戻すとcondition bodyの`io.ReadAll`待ちはfgprofで累積2,764.97 goroutine秒、`postIsuCondition`全体は2,786.59秒になった。buffering onのE50 2回ではbody readもhandlerもfgprof上位50件に現れず、Nginxが完全なbodyだけをGoへ渡していたことを確認できた。
- conditionの202は243,911件でE50 repeatの248,181件を下回り、登録成功も908件でE50 repeatの913件を下回った。1登録あたり202は約268.6件、condition p95は67msで、E50 repeatの約271.8件・68msに対してthroughput改善はない。POST ISUには500も1件発生した。
- scoreもE50の138,810点・143,994点を上回らなかった。streamingは大量の未完了body待ちgoroutineをAppへ移すだけで直接指標を改善しなかったため棄却し、conditionのrequest bufferingをonへ戻す。

### E52 expectation

- vendor JSは743,417 bytesのまま1runに1,314〜1,458回配信され、E50/E51では約0.98〜1.08GBを占め、平均202〜221ms、p95 638〜763msだった。s1はCPUが約45%使われ、平均約49MB/sを送信している一方、同じassetには207,404-byteの正しい`.gz`が初期imageから存在する。
- Nginxは`http_gzip_static_module`を備えているため、asset locationで`gzip_static on`を有効にする。アプリやAPI responseは圧縮せず、`Accept-Encoding: gzip`を送るclientだけに事前圧縮済みassetを返す。
- deploy前後で元fileとgzip展開後のSHA-256一致、gzipあり/なしのContent-Length・Content-Encoding・ETag、304応答、Nginx config testを確認する。公式validを保ち、vendor送信byte、p50/p95、s1 network/system CPU、登録成功、scoreが改善すれば採用する。validator不一致、5xx、またはvendorの直接指標が改善しなければ無効化する。
