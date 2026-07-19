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
| E31 | 18:14 | pending | condition request bodyのbufferをpool再利用 | E30 heapで総alloc 4.24GiB中`io.ReadAll`が1.25GiB（29.5%）。終了時にも19.4MiB保持 | pending | pending | 256KiB以下だけpoolへ戻し巨大buffer滞留を防ぐ。入力alias test、validator必須。alloc/GC/score悪化なら戻す |

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

## Current hypothesis queue

1. `isu_condition(jia_isu_uuid, timestamp)`の複合indexで全表走査を除く。
2. graphを要求日の24時間へSQLで絞り、trendの全履歴取得を最新1件へ縮める。
3. 不要なimage BLOB転送とrequest/debug logを除く。
4. condition POSTをmulti-row化してからdrop率を下げ、得点機会を増やす。
5. 実測後にN+1解消、latest condition構造、hour aggregate、3台構成を判断する。

各変更前に、期待する計測変化とrollback条件をこの表へ追記する。
