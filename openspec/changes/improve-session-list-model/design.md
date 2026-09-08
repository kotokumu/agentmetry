> **Scope:** This file applies only to this Change. After archive, it is out of scope.

## Context

- Risk: High（sourcepluginの公開契約追加と候補取得用DBインデックス追加）。入力はテレメトリーのみ。世代・取り込み解釈・既存の行は変更しない。名前は保存済みデータを読み取り時に解釈する。
- Human direction: 「タスクをすべて終わらせてPR作ったらmainにマージして」に加え、2026-09-06の「テレメトリーデータ以外は使えない」「それ前提で進めて」を適用する。
- Evidence packet v5とCodex観測名の対象範囲承認を適用する。Claude生成名はPR #59で提供済み。2026-09-09にユーザーは部分index・forward-fixの設計に「はい、PR出してマージするところまで」と承認した。独立設計レビューcodex_name_design_review_v5は旧版起動経路のP2修正を再確認し、残る指摘なし。全件取得・未観測の改名・実画面照合の未完了は維持する。

---

## Goals / Non-Goals

### Goals

- 既存のprovider正規化と関係解決を再利用し、一覧表示単位を一つの指定で切り替える。
- 一覧と詳細の状態・集計を分離する。
- 既存データで動作し、非テレメトリー依存を追加しない。

### Non-Goals

- 改名同期、タイトル用の独立したDB、別のprovider registry、SDK adapter、新しい詳細エンドポイント。

---

## Conceptual-Model-to-Implementation Mapping

| Specification concept / Requirement | Owning component | Physical representation | Notes |
|---|---|---|---|
| Native Conversation | existing source plugins / query | 既存のsource + run ID / ConversationIdentity | 別のcanonical identity packageを作らない |
| Evidence / Membership | existing ingestion / SQLite session graph | session_links / session_memberships | 根拠抽出と関係解決は別責務、raw retentionは別境界 |
| View | query | SessionListView値、既定ROOTS | 値の検証はquery、wire数値変換はtransport |
| Unit / row | query / SQLite | SessionListEntry（Session埋込、root/parent IDs） | 受動的なread projection。Roleは親有無から導く |
| List query | SQLite | rollupのgroup key切替 | source修飾、検索・構造化条件も同じ単位 |
| List wire | Connect transport / proto | additive fields | SQLやprovider DTOをqueryに漏らさない |
| Browser list state | SessionListController | query、rows、next token、request generation | 詳細controllerから分離 |
| Browser catalog mapping | API adapter | 純粋な変換／検証関数 | protobufをUI controllerへ渡さない |
| URL | existing navigation / app shell | view query parameter | 保存filterとは別 |

---

## Decisions

### Decision: Reuse projected membership

- **Choice**: ROOTSのgroup keyは既存のroot ID、ALLはrun ID。直接親・ルートは解決済みmembershipから取得する。
- **Rationale**: 生のリンクからもう一度親を推論すると競合・循環の扱いが二重化する。
- **Alternatives**: agent IDをsession化する案はClaudeの同一性を破る。unknown役割の追加は「観測上のroot」と人間作成の軸を混ぜる。
- **Consequences**: 全ての派生filterと検索のgroup keyを同じViewに従わせる。詳細・dashboardは既存のROOTS処理を維持する。

### Decision: Additive list contract

- **Choice**: Proto SessionListViewはUNSPECIFIED=0、ROOTS=1、ALL=2。request field 4=view、response field 4=applied_view。SessionSummary field 11=catalog。SessionCatalogはrole=1、root_session_id=2、parent_session_id=3。SessionRoleはUNSPECIFIED=0、ROOT=1、CHILD=2。
- **Rationale**: 旧clientは追加フィールドを無視し、旧serverはALLを確認できない。名前metadataは後述の追加契約を使う。
- **Consequences**: domain SessionPageは一覧専用SessionListEntry列とAppliedViewを持つ。詳細Sessionの表現は不変。旧Go呼び出しのゼロViewはROOTS。MCPは明示ROOTS。
- **Errors**: 不明ViewはInvalidArgument。既存のfilter/page validationとエラー契約を維持する。

### Decision: List state owns its requests

- **Choice**: SessionListControllerは狭い一覧readerとhost、filter/view supplierを受け取り、Lit lifecycle、refresh、loadMoreを所有する。ConversationsControllerは一覧を委譲し、詳細・活動・reworkを引き続き所有する。
- **Rationale**: 選択詳細のroot集約をALLの単独行として再利用してはいけない。
- **Consequences**: APIの既存listSessionsはROOTS wrapperとして維持し、page取得契約を追加。readerの結果はrowsとnext token、旧server不適合は固定分類の失敗。行のrole/root/parentは純粋mapperで検証する。未知catalogは役割表示をしない。ALLの不正行は全体を拒否する。
- **Lifecycle**: filter/view変更はrows/tokenをクリア。refreshは現在rowsを保持して先頭を再取得し、成功時置換。loadMoreは次tokenがありprimary要求なしの時だけ実行。同じ要求をcoalesceし、generationが古い応答を無視する。失敗時は固定ラベルで再試行可能。disconnectでabortと世代更新。
- **Comparison**: 比較候補は既存のROOTS単位を維持する。workspaceはALL表示中だけ別のROOTS list controllerを有効化し、ROOTS表示では可視一覧を共有する。activation supplierは非表示時に要求・行・tokenを破棄し、再有効化時に再取得する。一覧に影響するlive更新は両方の現在有効な一覧を更新する。子IDを比較候補へ渡したり、ALLの行をrootの集計として扱ったりしない。

---

## Responsibility and Boundary Review

| Responsibility / Boundary | Owner / authority | Consumer / constraint | Dependency direction | Simpler alternative / verdict |
|---|---|---|---|---|
| provider解釈 | source plugin、受信属性 | ingest、provider差 | ingest→sourceplugin | 既存を再利用 |
| membership解決 | SQLite session graph、リンク集合 | queries、競合／循環整合性 | SQLite→query | 別resolver不要 |
| Viewの意味 | query値、選択条件 | transportとSQLite | adapters→query | boolでなく二値enum |
| 一覧snapshot | SQLite read transaction | API、関係と活動の整合性 | transport→reader | 既存transactionを再利用 |
| wire mapping | Connect / Web API | 旧新peer、互換性 | adapter→domain/UI model | 新service不要 |
| 最新要求 | list controller | UI、lifecycle／race | component→controller→reader | stateless関数では継続状態を守れない |
| 表示／URL | component / navigation | 利用者、履歴とa11y | UI→list contract | controllerでhistory操作しない |

SRP: provider・関係・一覧状態・表示を各所有者に置く。OCP: 名前解釈は既存registryのoptional拡張に限定し、別registryを作らない。LSP: wrapperと既定ROOTSを維持。ISP: 一覧readerは一操作。DIP: domainはSQL/proto/SDKに依存しない。

---

## Independent Evolution Scenario Impact

独立agent telemetry_evolution_scenariosはpacket v2のみを読み、モデル・設計を参照しない。初期minimality判定後に次のシナリオを適用する。

| Scenario / Confidence | Primary owner | Expected propagation | Unexplained / duplicated policy | Verdict |
|---|---|---|---|---|
| S1 View切替 / Committed | query View / list controller | queryとUI、tests | なし | Pass |
| S2 source同一ID / Observed | native identity | source修飾したmapper、row key | なし | Pass |
| S3 Claude同一session / Observed | Claude plugin | normalization characterization | なし | Pass |
| S4 親欠落 / Plausible | projected membership | root表示と説明 | なし | Pass |
| S5 子と集約詳細 / Committed | list/detail境界 | selected detailをALLへ混入しない | なし | Pass |
| S6 子だけ条件一致 / Plausible | list unit | 検索・structured条件・pagination | なし | Pass |
| S7 遅延関係 / Plausible | existing projection feed / list refresh | 先頭ページを現在Viewで更新 | なし | Pass |
| S8 ページ境界と更新 / Plausible | list controller | token reset、duplicate防止 | なし | Pass |
| S9 旧URL/client / Observed | navigation / transport | default ROOTS、ALL acknowledgement | なし | Pass |
| S10 混在した過去関係 / Plausible | existing memberships | migrationなしのread projection | なし | Pass |
| S11 表示名欠落 / Observed | presentation | native IDと制限説明 | なし | Pass |
| S12 将来上流契約追加 / Speculative | provider contract | 再調査の契機のみ | 拡張点なし | Risk only |

PlausibleはEvidence-backed plausibleを表す。適用後もconcept追加は不要。S6はUnit全体で条件判定、S7は次回snapshot、S9は省略ROOTSと要件で確定する。

---

## Interfaces and Test Specification

| Contract / consumer | Input / result / error | Observable test | Hidden detail |
|---|---|---|---|
| query.SessionListView / adapters | zero→ROOTS、ALL、不明値→typed error | default/explicit/invalid table | protobuf numeric domain |
| query.SessionListEntry / transport | Session + root/parent、roleは親から導出 | ROOT/CHILD metadata、detail不変 | SQL membership |
| SessionListReader / API,MCP | filter + View → SessionPage、storage error | default/ALL/search/conditions/page | transaction・SQL |
| listSessionsPage / list controller | range/source/search/conditions/view/token + signal → mapped page | ALL acknowledgement、malformed metadata、ID fallback | protobuf・Connect |
| SessionListController / workspace | supplier + narrow reader、refresh/loadMore | race、dedup、disconnect、live refresh | wire、URL、localization |
| navigation / app | URL↔view、session target→URL | duplicates/invalid/default、reload/history | reader |

新しい関数は実際のconsumerを持つ場合にだけexportする。公開classは一覧の要求世代・接続状態を守るものだけ。SessionListEntryは受動的結果であり別entityを作らない。

### TDD and Construction Units

| Implementation Unit | Behavior / Given → When → Then | Construction mode | Smallest representation / refactor target | Migration / rollback |
|---|---|---|---|---|
| source tests | Codex sent spawn/other → normalize → exact aliases、Claude同session複数agent → one会話 | baseline characterization | existing plugin tests、provider field shape fixture | なし |
| query + SQLite | R→C→G、別source同ID、孤立root → ROOTS/ALL → exact rows/counts/root/parent | Red-Green | enum + read projection、group keyを集約 | なし |
| SQLite filters | 子だけ一致、unobserved root、tie、空、複数page → query → Unit単位結果 | Red-Green | existing SQL境界を再利用 | なし |
| transport | old/invalid/ALL caller → ListSessions → acknowledgement/metadata or error | Red-Green | additive proto + mapper | old peer default |
| Web mapper/controller | deferred requests → switch/refresh/page/disconnect → latest consistent rows | Red-Green | pure mapper + stateful controller | old ROOTS response |
| Web navigation/components | keyboard切替、history、child selection → URLと行／詳細の整合性 | Red-Green | existing navigation/component拡張 | default ROOTS |
| docs / privacy | telemetry only仕様 → dependency/label audit → no enrichment | document verification | source limitations文書 | なし |

各テストは返却値・UI・リクエスト条件を検証し、内部helperの呼び出し順序は固定しない。性能は既存rollupの利用と代表fixtureのquery plan／時間を確認する。根拠のないベンチマーク閾値や新migration検証は要求しない。

---

## Risks / Trade-offs

- 親イベント未観測で子がROOTに残る → 説明文に観測上の分類と明記する。
- offsetページ間の更新で重複・抜けが発生し得る → 同一行をdedupし、ライブ更新では先頭から取得する。固定snapshot保証はしない。
- 現在の製品名との一致は未検証 → Claude生成名とCodex一覧結果の観測名を区別し、実画面照合を未完了で保持する。
- 独立レビューは実装前と実装後に行い、blocking findingを解消する。

---

## Migration / Rollback

Codex候補用の部分インデックスを追加する。generation・raw・活動の行は不変。既存Atlas convergenceのAddIndexを利用し、取り込み開始前のtransactionで作成する。失敗時は起動を失敗させ、索引なしの高負荷な一覧取得へ切り替えない。

| Compatibility / operation | Validation | Failure / rollback | Owner / approval |
|---|---|---|---|
| 更新前DBから更新版へ | 一時DBで既存データ・generation・名前取得、再openのschema差分なしを検証 | 作成失敗はtransaction rollback、再起動で再試行 | SQLite。2026-09-09ユーザー承認済み |
| インデックス追加後の不具合 | 索引を維持し、名前解釈の修正または無効化を行う更新版で確認 | 原則forward-fix。rawの再取り込み不要 | Agentmetryの修正版 |
| 更新前binaryへ戻す | 低位sqlite.OpenのDropIndex拒否と、実起動経路MigrateIfNeededの再投影判定を一時DBで区別して確認 | 旧版の実起動は追加indexを差分とみなし、全journal再投影・DB置換を行い得る。直接downgradeを通常のrollbackにしない。必要なら停止後に追加indexだけを除去する別途承認済み手順を用いる | 運用者。利用者DBではこの作業を実行しない |

索引構築には既存logsの一回走査と追加ディスクが必要である。検証時は一時DBだけに書き込み、利用者DBでは読取確認に限定する。raw再投影・compactionに独立した名前のauthorityを追加しない。

現行の起動は[main.go](https://github.com/kotokumu/agentmetry/blob/main/cmd/agentmetry/main.go)から[compaction](https://github.com/kotokumu/agentmetry/blob/main/internal/compaction/compaction.go)を経由してSQLiteを開く。rollback評価を[低位schema convergence](https://github.com/kotokumu/agentmetry/blob/main/internal/storage/sqlite/migration.go)だけで完結させない。

---

## Open Questions

ROOTS/ALLの未解決事項はない。追加の名前表示についてはmodel D-3〜D-5（Codex全件、手動改名、実画面照合）を未解決として維持する。

---

## ROOTS/ALL Verification Results

2026-09-06、実装・検証タスク完了。[PR #55](https://github.com/kotokumu/agentmetry/pull/55)はWeb・Go・統合テスト・Desktop build inputs成功後にmainへマージ済み。

- `go test ./...`、`go test -tags=integration ./...`: pass。
- `go test -race ./internal/query ./internal/storage/sqlite ./internal/transport/connectapi ./internal/transport/mcpserver`: pass。
- Web 28 files / 293 tests、production build: pass。app回帰は構造化filterとALL維持、比較候補のloading/failure/retry、子URLとroot詳細、履歴復元を含む。
- `buf lint`、`buf breaking --against '.git#branch=origin/main'`、再生成、OpenSpec strict validation、`git diff --check`: pass。
- 一時DBの実ブラウザーでSpaceキーによるALL切替、reload後の保持、backでROOTS復元、日本語の制限説明を確認。既存の利用者DBへ書き込んでいない。
- ローカルDBのread-only `EXPLAIN QUERY PLAN`: ROOTSはrollup走査＋membership主キー検索＋group/order用一時B-tree、ALLはrollup走査＋membership主キー検索＋order用一時B-tree。既定一覧はlog/bodyを走査せず、新indexやmigrationを追加する根拠はない。
- 独立レビューはモデル・責務・境界・interface・tests・実装品質を確認。ALLでのfilter復元と比較候補の単位不一致を修正し、再レビューにblocking findingなし。比較のID整合性検証は緩和していない。
- 変更依存のaudit: SDK、セッションファイル、app-server、hook、タイトル生成workerを追加していない。DB schema/generationは不変。

---

## Name Observation Design

この節はClaude生成名とCodex観測名の設計をまとめる。検証記録は取得元ごとに区別する。

### Responsibility and Interface Mapping

| Concept / contract | Owner / consumer | Signature or physical representation | Constraint / simpler alternative |
|---|---|---|---|
| 名前の観測 | Claude plugin / Registry | optional SessionNameExtractor: SessionName(Event) (SessionName, bool)。値はConversationID, Text, Origin, ObservedAt | provider解釈は既存pluginに置く。必須Plugin契約は変えない |
| 複数の名前の観測 | Codex plugin / Registry | optional SessionNamesExtractor: SessionNames(Event) []SessionName | 一覧結果が複数対象を持つため。一件へ制限するとSC-9を満たせない |
| source選択 | Registry / SQLite | SessionNames(sourceID, Event) []SessionName | 複数契約を優先し、未実装なら既存の単数契約へfallback。両方を呼んで二重化しない。非対応sourceは空 |
| 名前選択 | query / SQLite | SelectSessionName([]SessionName) *SessionName。値はText, Origin, ObservedAt | pure function。SQLやUIに新旧判断を置かない |
| 一覧結果 | query / Connect | SessionListEntry.Name *SessionName | 会話identityと詳細Sessionは不変 |
| wire | Connect / Web | 既存SessionCatalog.nameとSessionNameを再利用。originにcodex_app.list_threadsを追加 | protoのフィールド追加なし。旧Webは未知originの名前だけを無視 |
| label | Web mapper / component | optional catalog.name | 名称・由来・ID・観測時刻を確認可能。URLと選択はID |

呼出関係: Registryの複数抽出 → ページ内の対象会話IDへ対応付け → queryの名前選択。旧SessionNameメソッドと単数契約を残し、既存pluginのsource互換性を維持する。抽出は入力不変・外部I/Oなし。無効な根拠は空の結果、DBエラーはSQLiteから返す。

### Decision: Read-time interpretation

- Claude pluginはassistant_responseかつquery_source=generate_session_titleだけを解釈する。session.idが必須で、canonical conversation IDがある場合は一致が必要。
- native OTLP EventNameだけを持つ入力は保存前にgen_ai.response.completedへ正規化される。event.name属性がない場合に限り、この保存形状も受理する。明示的な異なるevent.name属性は上書きしない。
- responseは完結したJSON objectで、空白だけでないtitle文字列を持つこと。値を要約・加工しない。省略マーカー、重複titleキー、非文字列、壊れたJSONは無効。
- 時刻はevent.timestampのRFC3339。欠落・不正・UTC換算で年1〜9999の範囲外なら不明であり、受信時刻で代替しない。originはclaude_code.generate_session_title。
- 不正な名前は名前の根拠だけ無視する。既存のrawや活動の投影は変更しない。

### Decision: Codex list-result interpretation

- source=codex、event.name=codex.tool_result（属性省略時は正規化済みgen_ai.tool_resultも可）、tool_namespace=mcp__codex_app、tool_name=list_threads、successがtrueの結果に限定する。明示的に矛盾するイベント名・namespaceは受理しない。
- outputの既知包絡は`Wall time: <非負の数値> seconds`と改行後の`Output:`である。包絡なしのJSON objectも受理する。任意の本文からJSONらしい部分を検索しない。
- schemaVersion=4のトップレベルpinnedThreads/threads配列のみを走査する。未知バージョン、重複schemaVersionや同じ配列キー、完全なJSONの後の余分な本文は結果全体を無効とする。他のトップレベル値は正しく読み飛ばす。
- 各項目はJSON objectとして完結し、kind=codex、非空の文字列id、空白だけでない文字列titleを持つこと。id/kind/titleの重複キー、マスク・省略マーカーを含む値は無効とする。任意のsummaryや入れ子から名前を拾わない。ChatGPT項目は無視する。
- output_truncated=trueかつ末尾が既知の`[... telemetry preview truncated ...]`の場合、マーカーを除いたJSON prefixを逐次解釈する。EOFによる未完結に限って、確定済みの先行項目を返す。構文エラーでの途中復旧はしない。不完全な最後の項目は、id/titleだけ先に読めても採用しない。
- 同じ結果内の同一IDの別項目も観測として保持し、競合の判定はqueryの既存選択に委ねる。対象IDと実行元conversation.idが異なることは正常である。source修飾はRegistryが選択したcodexを使い、結果内のkindだけでsourceを変更しない。
- originはcodex_app.list_threads。時刻はevent.timestampのRFC3339Nano、欠落・不正・UTC年1〜9999外は不明。項目のupdatedAt、ログ受信時刻、走査順を新旧判断に使わない。省略された未観測部分や削除・改名を推測しない。

### Decision: Page-scoped lookup

- 既存rollupでページを確定後、同じread transaction内で表示行のClaude sourceとIDに一致するlogs候補を取得する。期間より前の生成名も対象とする。
- query_sourceのSQL絞り込みは取得量削減であり、結果はpluginで再検証する。wire形状変更時は候補queryとpluginの適合テストへの波及が必要。名前の有効性・選択規則はSQLに置かない。
- 既存source/run indexを利用し、ページ外の会話を読まない。会話内検索の費用は残るため、代表query planと実データ読み取り時間を記録する。
- DBエラーは一覧全体の失敗。子の名前をrootへ転用しない。backfill・DB migration・workerは不要。

Claudeの候補取得は上記のまま維持する。Codexは対象IDが別会話の結果内にあるため、実行元run_idで絞り込まない。

- `logs`のidを対象に、`source = 'codex' AND tool_name = 'list_threads'`の部分インデックス`logs_codex_session_names_idx`をschema.hclへ追加する。namespace・結果構造・名前の妥当性は索引条件ではなくCodex pluginが検証する。
- ページにCodex行がなければ読取を省略する。ある場合は同一read transactionでインデックス条件に合う保存済み結果を走査し、逐次抽出した観測のうちページ内対象IDだけを保持する。日時の下限・候補件数の上限で過去の根拠を黙って落とさない。
- 名前のない会話や結果内で初めて現れる会話の行を作らない。ルートがページ内にあり、その名前が子のツール結果に含まれる場合は、項目の対象IDがルートと一致するときだけ採用する。
- source indexによる全Codex活動の走査は不要な本文読取が多い。専用の名前テーブル・backfill workerは追加状態と同期が必要になるため採らない。部分indexと既存read-time境界を採る。
- 候補結果数に比例する取得費用は残る。一時DBのEXPLAINで部分index利用を検証し、候補の少ない通常活動を大量に含むfixtureで読取時間と索引構築時間を記録する。実DBへ索引を試作しない。
- 統計未作成の一時DBではsource indexが選択されたため、候補queryにINDEXED BYを指定する。部分index欠落は要求失敗とし、全Codexログ走査へfallbackしない。

### Decision: Order-independent selection

- 最新の既知時刻と時刻不明の候補を比較する。候補の名前・取得元が異なればnil。全て同名なら採用し、不明時刻を含めば返却時刻も不明とする。
- 既知の古い候補は最新候補の競合に影響しない。重複・到着順に依存せず、入力を変更しない。
- UIはsourceとoriginの組を検証する。Claudeは「自動生成名」、Codexは「観測名」と表示し、Codex名が一覧取得結果由来であることを説明する。native IDと観測時刻（あれば）を維持する。現在の画面名への同期は保証せず、未知origin・不正metadata・旧serverはID表示。HTMLではなくテキストとして描画する。

### Codex Responsibility and Boundary Check

| Responsibility / boundary | Owner / authority | Consumer / constraint | Expected dependency | Simpler alternative / decision |
|---|---|---|---|---|
| 複数対象のprovider解釈 | Codex plugin、既知の受信形状 | SQLite、誤った会話への転用防止 | SQLite→Registry→plugin | 必須Plugin変更を避けoptional契約を追加 |
| 実行元と対象の分離 | 名前の観測、対象ID | SQLite、source/ID不変 | storage→query値 | 実行元への別名扱いは誤り |
| 最新/競合 | queryの名前選択 | storage、重複/順序非依存 | storage→query | SQLとUIで規則を複製しない |
| 候補探索とsnapshot | SQLite、保存logs | list caller、既存データと整合性 | adapter→storage | 部分index。別キャッシュ管理者は不要 |
| 意味の表示と互換性 | Web mapper/component、origin | 利用者、生成名との区別 | UI→API adapter | 既存のoptional metadataを再利用 |

SRPはprovider形状・名前選択・DB探索・表示に分離する。OCP/ISPは既存単数pluginを壊さないoptional複数契約に限定する。LSPは単数fallbackと旧APIのID表示を維持する。DIPはquery/sourcepluginをSQLite・protobuf・SDKから独立させる。

### Codex Independent Scenario Stress Test

codex_name_scenarios_v5は凍結packet v5だけからシナリオを作成し、初期minimality記録後に開示した。

| Scenarios / confidence | Owner | Expected propagation | Unexplained impact / verdict |
|---|---|---|---|
| S1実行元と複数対象 / Observed | 名前の観測、Registry、SQLite | 複数抽出と対象IDで対応 | なし / Pass |
| S2混在種別・Claude、S3一部未観測 / Observed・Committed | Codex解釈、Web | kind/source分離、ID fallback | なし / Pass |
| S4内容競合、S6再送・逆順 / Committed・Evidence-backed plausible | query | 既存の観測時刻選択を再利用 | なし / Pass |
| S5末尾省略位置 / Observed・Evidence-backed plausible | Codex解釈 | prefix内の完結項目と未完結項目を区別 | なし / Pass |
| S7既存16GB、S10実DB不変 / Observed・Committed | SQLite / 検証 | 部分index、起動時構築の失敗と一時DB検証 | ユーザー承認済み / Design pass |
| S8追加収集禁止 / Committed | 外部境界 | 既存logsだけを読み外部呼出なし | なし / Pass |
| S9ROOTS/ALL・URL・集計 / Committed | 一覧単位、UI | page決定後のlabelのみ変更 | なし / Pass |
| S11実画面正解fixture / Committed | 受入検証 | 名前観測のテストと実画面の証明を分離 | 未完了を維持 |

適用後のminimality再確認でも新しい会話種別・同期lifecycle・managerは不要。schemaVersionの将来変更や別保存技術への移行は根拠がなく、拡張点を追加しない。

### Codex Interfaces, Test Specification and Construction Plan

| Unit / contract | Requirement / Given → When → Then | Simplest representation / hidden detail | Migration / rollback |
|---|---|---|---|
| sourceplugin.SessionNamesExtractor / Registry.SessionNames | SC-9、一件/複数/非対応plugin→抽出→所有sourceだけの観測列 | optional interface、既存単数fallback、入力不変。DB/protoを隠す | 旧plugin維持 |
| Codex plugin | SC-9/10、混在・完全/末尾省略・未知schema・偽namespace・失敗・重複キー→抽出→正しい対象と文字列だけ | 無状態関数と既存Plugin。JSON形状をprovider内に隠す | raw不変 |
| query.SelectSessionName | SC-9/10、同じ対象の同時刻競合・逆順・未知時刻→一覧→規則通りの名前かID | 既存pure functionを再利用。entry.updatedAt非利用も検証 | 規則変更なし |
| SQLite/schema | SC-9、A内にB/C名・Aが期間外/別page・同ID別source・親未活動→normalize/store/reopen/list→正しい名前と既存行/集計 | 部分indexと既存transaction。名前だけで行を作らない | 上記Migration表 |
| Connect/Web | SC-11、Codex origin・Claude origin・未知/不正/source不一致・HTML→API/画面→観測名/生成名/IDを正しく区別 | 既存protoとmapper/component。URL・選択・狭幅の最新UIを維持 | 旧WebはCodex名を無視 |
| privacy/acceptance | SC-5/7/8、依存とfixtureを監査→非telemetry入力なし・実画面照合を未完了で記録 | 構造fixtureは匿名化。新しいlive呼出や実DB更新なし | archiveしない |

新規挙動は各単位でRed→Green→Refactor。queryとClaudeは既存回帰を維持する。名前の返却値・UI・ID/集計を検証し、private helperの順序を固定しない。Go全体/統合/race、Web tests/build、buf lint/breaking、OpenSpec strict、diff check、独立実装レビューを完了条件とする。

### Codex Construction and Verification

- Go全体、integration全体、sourceplugin/Codex/Claude/query/SQLite/Connect/compactionのrace、buf lint/breaking、OpenSpec strict、diff checkは成功。main a9146e40を取り込んだ状態でGo全体・integrationを再確認し、Web 37 files / 388 testsとproduction buildも成功。

| Unit | Red | Green / refactor evidence |
|---|---|---|
| Registry | 単数fallback・複数対象の期待にnilで失敗 | optional契約を優先し、所有sourceだけへ委譲。入力不変と旧単数APIを確認 |
| Codex plugin | 有効な受信結果で観測列が空となり失敗 | 38ケース成功。抽出はprovider内に閉じ、完結項目・明示省略・重複キー・偽namespace・kind・時刻を確認 |
| SQLite | 別会話内の対象名がnilとなり失敗 | normalize→store→reopenでROOTS/ALL、同ID別source、期間外実行元、ページング、集計不変、末尾省略、遅着した古い名前を確認 |
| Index | 追加前はwrite denialでconvergenceが成功し失敗。追加後は既存source indexを選ぶplanで失敗 | INDEXED BYで部分indexを使用。作成失敗、データ/generation保持、再試行、再openの無差分を確認 |
| Web | Codex metadataが破棄され、badgeがGenerated nameとなり失敗 | source/originの組を受理し、日英の観測名、安全なテキスト、元ID・時刻・選択・URLを確認 |

- 一時DBの100,000 logs・候補10件でschema convergenceと索引構築は単回19.716375ms、候補読取は34.291µs。planはSCAN logs USING INDEX logs_codex_session_names_idx。これは代表fixtureの参考値であり、利用者の16GB DBの時間保証ではない。
- 旧版相当のDropIndex差分について、低位Openの拒否とRequiresProjectionRebuild=trueを確認。起動経路のMigrateIfNeededでもjournal再投影を確認し、hash・generationを保持する。実旧binaryの試験とは区別する。
- 独立実装レビューcodex_name_design_review_v5はモデル・最小性・責務・interface・parser・storage・Webを確認し、P0/P1/P2の指摘なし。名前選択をqueryへ集約し、新しい会話種別・同期状態・managerは追加しない。
- 利用者DB・telemetry設定を更新せず、一時DBだけで検証する。SDK、session files、app-server、追加の一覧取得で補完しない。D-3〜D-5と実画面の受入fixtureは未完了を維持する。

### Independent Scenario Stress Test

title_scenarios_v4は要件と実観測のみから15シナリオを生成し、初期minimality確認後に提示する。同じ外部変化は表内で集約する。

| Scenarios / confidence | Owner | Expected propagation | Verdict |
|---|---|---|---|
| source同ID、Claude同会話、親活動なし / Committed | native identity / SQLite | 行の会話だけに名前を対応。ROOTS/ALL回帰 | Pass |
| 生成応答・欠損・マスク・設定off / Observed・Evidence-backed plausible | Claude plugin | parserとfixture。設定変更なし | Pass |
| 更新・同時刻競合・重複・逆順 / Committed・Evidence-backed plausible | query | 観測集合の選択テスト。SQL受信順を使わない | Pass |
| 由来・未送信改名・部分Codex / Committed・Observed | UI / 対応範囲 | 自動生成名の説明、Codexと改名は未完了 | Pass for bounded PR |
| 旧peer / Evidence-backed plausible | proto / mapper | additive metadata、detail省略、ID fallback | Pass |
| 補完禁止・実DB不変・未完了PR / Committed | 入力 / 検証境界 | 一時DBテスト、実DB read-only、draft PR | Pass |

全件性・異種根拠の優先権・将来providerの具体仕様には根拠がなく、同期機構や新しい拡張点を要求しない。責務と境界を変更せず全シナリオを再適用できる。構築前の独立設計レビューでP0/P1なしを確認済み。

### Claude Test Specification and TDD Plan

| Unit / criterion | Given → When → Then | Construction / simplest representation |
|---|---|---|
| sourceplugin / Claude | 実観測形状・通常応答・無効JSON・重複titleキー・ID矛盾・時刻欠落 → extract → 正しい観測か無名 | gotests table Red→Green。値と既存pluginだけ |
| query | 最新・古い競合・未知時刻・同時刻競合・再送 → select → 順序非依存の名前かnil | table Red→Green。pure function、状態manager不要 |
| SQLite | 過去保存済みOTLP・同ID別source・親子 → ROOTS/ALL → 正しい名前と既存の行/集計 | integration Red→Green。既存transaction/index |
| transport / Web | optional name・未知origin・旧peer・HTML文字列 → map/render → 名前・由来・ID、互換安全性 | mapper/UI Red→Green。生成コードは再生成 |
| SC-7 | 実画面と同一会話のtelemetry → 照合 → 対応範囲の証明 | 未完了。匿名化構造fixtureでは代替しない |
| SC-8 | PR/文書 → audit → Codex/改名/実画面照合を未完了で記載 | document verification |

Claude実装分のSC-1〜5は既存回帰、SC-6は上記automated testsで検証する。Claude分にはDB schema/世代/取り込み挙動の変更はない。Codex分のmigration検証と追加の完了条件はCodexの構築計画に従う。

### Generated Name Verification Results

2026-09-08、draft PR対象の検証結果。名前表示要望全体の完了記録ではない。

- Registry 5、Claude抽出28、query選択22ケースでRed→Green。最新/未知時刻の全6順列、入力不変、重複・競合、異なるsourceへの委譲禁止を含む。
- 一時DBのOTLP正規化→保存→再open→一覧でROOTS/ALL、同じIDの別source、親への子名転用禁止、期間外の生成時刻、native OTLP EventNameの保存互換性を確認。実データのID・タイトルはfixtureに含めていない。
- `go test ./...`、`go test -tags=integration ./...`、SQLite/Connect/query/Claude/sourcepluginのrace: pass。
- Web全28 files / 306 tests、production build: pass。HTML文字列の安全表示、日英、時刻の有無、名前更新後の選択・URL維持を含む。
- `buf lint`、`buf breaking --against '.git#ref=origin/main'`、生成物再生成のSHA-256一致、OpenSpec strict、`git diff --check`: pass。
- 独立実装レビューのP2を2件修正した。native EventNameの正規化後の取得漏れは統合テスト、UTC換算で範囲外になる時刻は抽出とprotojson往復テストで回帰を確認。再レビューで残る指摘なし。
- 利用者DBのread-only EXPLAINは`logs_source_run_usage_idx (source=? AND run_id=?)`を使用し、logs全体を走査しなかった。最近のClaude native会話100件に絞った候補COUNTは91件、単回wall time 1.78秒。これは候補queryだけの参考値であり、API全体の性能保証ではない。会話内のログ量に比例する読取費用は残る。
- DB・telemetry設定の書換え、SDK/セッションファイル/app-server/hookによる補完は行っていない。SC-7の実画面正解fixture、Codex全件名、手動改名追跡は未完了のまま維持する。
