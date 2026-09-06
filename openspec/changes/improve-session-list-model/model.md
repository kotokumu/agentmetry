# Specification Analysis: improve-session-list-model

## 1. Boundary

| Item | Decision | Evidence |
|---|---|---|
| Product capability | session-catalog | 一覧の表示単位を利用者が選ぶ |
| Change classification | behavior change | 全件表示の追加 |
| Included behavior | テレメトリー由来の同一性と親子を使う一覧 | User constraint 2026-09-06 |
| Excluded behavior | 表示名の別経路補完、合成セッション | 同制約、evidence packet v2 |

---

## 2. Consumers and Observable Events

| Consumer / actor | Trigger / prior state | Interaction or event | Observable result |
|---|---|---|---|
| 利用者／API caller | 会話活動が存在 | 通常一覧を開く | 親子をまとめたルート一覧 |
| 利用者 | 通常一覧 | 全件表示を選ぶ | 活動がある各native会話と観測上の役割 |
| 利用者 | 子の行が存在 | 行を選ぶ | 既存の親子集約詳細。単独行は維持 |
| OTLP exporter | 既存の親子情報 | 関係を追加する | 次回一覧／ライブ更新に解決済み関係を反映 |

---

## 3. Concept Analysis

| Concept or change | Specification decision | Evidence | Owner capability and rationale |
|---|---|---|---|
| Native Conversation | sourceとnative IDの組 | Codex conversation.id / Claude session.id | session-catalog: 行の同一性 |
| Session Link Evidence | 観測された有向関係。親確定とは別 | 既存リンク競合・循環処理 | session-catalog: 関係の根拠 |
| Projected Membership | 解決済みの親・ルート | 既存session graph | session-catalog: 一覧単位の根拠 |
| Session List View / Unit | ROOTSは成分、ALLは単独会話 | SC-1, SC-4 | session-catalog: 選択単位 |

### 3-1. Minimality Check

| Candidate | Semantic Remove / Merge Test | Decision | Reason |
|---|---|---|---|
| Native Conversation | Agentと統合するとClaudeで会話が増える | Keep | 別の同一性 |
| Link Evidence / Membership | 統合すると競合した観測を確定親と扱う | Keep separate | 根拠と確定関係は異なる |
| View / Unit | 削除すると検索・件数の対象が曖昧 | Keep | 集約範囲の規則 |
| Human-created / Unknown role | テレメトリーに証明がない | Reject | 推論を事実としない |
| Name Authority / Enrichment Lifecycle | 必要な入力が禁止されている | Reject | 現在の要件を実現しない |
| Catalog manager / provider factory | 意味の追加がない | Reject | 実装手順であり概念ではない |

初期minimality判定は独立進化シナリオを適用する前に作成する。Unitと一覧行は同一の受動的な読み取り結果として扱う。タイトル用の状態モデルは作らない。

---

## 4. Main Spec Conceptual Model Replacements

### `session-catalog`

```markdown
### Native Conversation

Native Conversationは一つのテレメトリーsource内の会話である。同一性はsourceとnative IDの組であり、異なるsourceの同じIDは別会話となる。Codexはconversation.id、Claude Codeはsession.idに対応する。Claudeのagent_idとparent_agent_idは会話内のagent関係であり、会話の同一性ではない。

### Concept: session-link-evidence

Session Link Evidenceは同一source内で観測した親会話から子会話への有向関係である。Projected Membershipはその根拠から解決した親とルートの関係であり、一つの会話は高々一つの親を持つ。成分は同じルートに属する会話の集合である。

RoleはProjected Membershipに基づく分類である。ROOTは解決済みの親がない会話、CHILDは解決済みの親がある会話を意味する。ROOTは人間が作成した証拠ではない。

### Session List View and Unit

| View | Unit |
|---|---|
| ROOTS | 成分全体。行の識別子はルート |
| ALL | 一つのNative Conversation。行の識別子はその会話 |

一覧の活動数・agent数・開始終了時刻と条件判定の対象はUnitである。会話活動は一覧対象のsemantic trace/log activityを指し、関係情報だけでは活動にならない。表示ラベルはnative IDであり、タイトルの観測を意味しない。

~~~mermaid
flowchart LR
    E[Session Link Evidence] --> M[Projected Membership]
    M --> R[ROOTS: component]
    M --> A[ALL: Native Conversation]
    N[Native Conversation activity] --> R
    N --> A
~~~
```

---

## 5. Requirement Candidates

| Requirement slug | Actor and event | Guarantee | Concepts used | Normative representations | Scenario tags |
|---|---|---|---|---|---|
| provider-native-session-identity | exporter→list caller | source修飾の会話単位を維持 | Native Conversation | invariant | happy, compatibility |
| evidence-backed-session-role | caller→list | 解決済み関係から役割を返す | Evidence, Membership | decision | happy, boundary |
| explicit-session-list-views | caller→query | 選んだ単位で検索・集計・ページング | View, Unit | decision | happy, boundary |
| telemetry-only-session-labels | user→list | ID表示、タイトルの推測なし | Native Conversation | invariant | happy, compatibility |
| session-list-view-negotiation | caller→API | 既定値と適用表示を明示 | View | partition | happy, error, compatibility |
| session-list-presentation | user→UI | 表示切替・履歴・最新要求と行の整合性 | View, Unit | prose | happy, concurrency |
| codex-event-normalization | exporter→ingest | 互換イベントの親子条件を維持 | Evidence | decision | happy, boundary, idempotency |

---

## 6. Unresolved Decisions

None.

---

## 7. Sources

- Evidence packet v2: telemetry-only constraint (user, 2026-09-06); evidence/provider-title-correlation.md records source references and limitations.
- [既存一覧](https://github.com/kotokumu/agentmetry/blob/main/internal/storage/sqlite/api.go)、[既存関係解決](https://github.com/kotokumu/agentmetry/blob/main/internal/storage/sqlite/session_graph.go)。raw保持は復元境界であり、表示名が存在する根拠ではない。
