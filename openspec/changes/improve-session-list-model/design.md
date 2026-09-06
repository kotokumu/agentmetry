> **Scope:** This file applies only to this Change. After archive, it is out of scope.

## Context

- Risk: High（一覧の公開API追加）。入力はテレメトリーのみ。DBスキーマ・世代・取り込み動作は変更しない。
- Human direction: 「タスクをすべて終わらせてPR作ったらmainにマージして」に加え、2026-09-06の「テレメトリーデータ以外は使えない」「それ前提で進めて」を適用する。
- Evidence packet v2は非テレメトリー補完案を無効とするmaterial revision。タイトル権威・収集worker・migrationの設計とタスクは削除する。

---

## Goals / Non-Goals

### Goals

- 既存のprovider正規化と関係解決を再利用し、一覧表示単位を一つの指定で切り替える。
- 一覧と詳細の状態・集計を分離する。
- 既存データで動作し、非テレメトリー依存を追加しない。

### Non-Goals

- 表示名同期、タイトル用のDB、provider registry、SDK adapter、新しい詳細エンドポイント。

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
- **Rationale**: 旧clientは追加フィールドを無視し、旧serverはALLを確認できない。名前用の未使用fieldは作らない。
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

SRP: provider・関係・一覧状態・表示を各所有者に置く。OCP: 未確定のprovider名対応には拡張点を作らない。LSP: wrapperと既定ROOTSを維持。ISP: 一覧readerは一操作。DIP: domainはSQL/proto/SDKに依存しない。

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
- 表示名は実現不能 → 別経路で補わずID表示と明示する。
- 独立レビューは実装前と実装後に行い、blocking findingを解消する。

---

## Migration / Rollback

DBスキーマ・generationは不変。旧binaryは同じDBを使用できる。変更のrevertは一覧のALL機能を除去し、既定ROOTSを残す。raw再投影・compactionに独立authorityを追加しない。

---

## Open Questions

None.

---

## Verification Results

2026-09-06、実装・検証タスク完了。[PR #55](https://github.com/kotokumu/agentmetry/pull/55)はWeb・Go・統合テスト・Desktop build inputs成功後にmainへマージ済み。

- `go test ./...`、`go test -tags=integration ./...`: pass。
- `go test -race ./internal/query ./internal/storage/sqlite ./internal/transport/connectapi ./internal/transport/mcpserver`: pass。
- Web 28 files / 293 tests、production build: pass。app回帰は構造化filterとALL維持、比較候補のloading/failure/retry、子URLとroot詳細、履歴復元を含む。
- `buf lint`、`buf breaking --against '.git#branch=origin/main'`、再生成、OpenSpec strict validation、`git diff --check`: pass。
- 一時DBの実ブラウザーでSpaceキーによるALL切替、reload後の保持、backでROOTS復元、日本語の制限説明を確認。既存の利用者DBへ書き込んでいない。
- ローカルDBのread-only `EXPLAIN QUERY PLAN`: ROOTSはrollup走査＋membership主キー検索＋group/order用一時B-tree、ALLはrollup走査＋membership主キー検索＋order用一時B-tree。既定一覧はlog/bodyを走査せず、新indexやmigrationを追加する根拠はない。
- 独立レビューはモデル・責務・境界・interface・tests・実装品質を確認。ALLでのfilter復元と比較候補の単位不一致を修正し、再レビューにblocking findingなし。比較のID整合性検証は緩和していない。
- 変更依存のaudit: SDK、セッションファイル、app-server、hook、タイトル生成workerを追加していない。DB schema/generationは不変。
