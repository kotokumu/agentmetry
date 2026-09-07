# Specification Analysis: improve-session-list-model

## 1. Boundary

| Item | Decision | Evidence |
|---|---|---|
| Product capability | session-catalog | 会話の識別、名前表示、一覧の表示単位を所有 |
| Change classification | behavior change | 名前表示の追加。ルート／全件の既存動作は維持 |
| Included behavior | テレメトリー由来の同一性・親子・名前を使う一覧 | proposal SC-1〜SC-8、モデル修正承認 2026-09-08 |
| Excluded behavior | 非テレメトリー入力、独自の名前生成、合成セッション | ユーザーの入力制約 |
| Risk / review scope | Highとして扱う。名前の公開契約と複数モジュールにまたがる責務への影響を設計で評価 | 本書は概念の修正。DB変更・公開API変更・構築の承認ではない |

---

## 2. Consumers and Observable Events

| Consumer / actor | Trigger / prior state | Interaction or event | Observable result |
|---|---|---|---|
| 利用者／API caller | 会話活動が存在 | 通常一覧を開く | 親子をまとめたルート一覧 |
| 利用者 | 通常一覧 | 全件表示を選ぶ | 活動がある各native会話と観測上の役割 |
| 利用者 | 子の行が存在 | 行を選ぶ | 既存の親子集約詳細。単独行は維持 |
| OTLP exporter | 既存の親子情報 | 関係を追加する | 次回一覧／ライブ更新に解決済み関係を反映 |
| 利用者 | Claude Codeのタイトル生成応答を受信済み | 一覧を開く | 対象会話に対応する名前。名前の観測と現在名の保証を混同しない |
| 利用者 | 名前を裏付けるテレメトリーがない | 一覧を開く | native IDと取得制限。通常の応答本文を名前に転用しない |
| 受入テストの検証者 | 同じ会話のテレメトリーと製品画面の証拠がある | 表示名を照合する | 対象製品・バージョン・画面・時点を限定した一致／不一致の結果 |

---

## 3. Concept Analysis

| Concept or change | Specification decision | Evidence | Owner capability and rationale |
|---|---|---|---|
| Native Conversation | sourceとnative IDの組 | Codex conversation.id / Claude session.id | session-catalog: 行の同一性 |
| Session Link Evidence | 観測された有向関係。親確定とは別 | 既存リンク競合・循環処理 | session-catalog: 関係の根拠 |
| Projected Membership | 解決済みの親・ルート | 既存session graph | session-catalog: 一覧単位の根拠 |
| Session List View / Unit | ROOTSは成分、ALLは単独会話 | SC-1, SC-4 | session-catalog: 選択単位 |
| 名前の観測 | 会話と、その会話について得た名前の根拠を分離 | SC-6、タイトル生成応答の実観測 | session-catalog: 名前の対象と根拠の意味を所有 |
| 一覧ラベル | 名前の観測を参照する表示値、またはnative IDによる代替表示 | SC-6, SC-8 | session-catalog: 表示値と同一性の関係を所有 |
| 実画面との照合結果 | 実行時の会話状態ではなく受入テストの証拠 | SC-7 | テストの検証記録。独立した製品capabilityにはしない |

### 3-1. Minimality Check

| Candidate | Semantic Remove / Merge Test | Decision | Reason |
|---|---|---|---|
| Native Conversation | Agentと統合するとClaudeで会話が増える | Keep | 別の同一性 |
| Link Evidence / Membership | 統合すると競合した観測を確定親と扱う | Keep separate | 根拠と確定関係は異なる |
| View / Unit | 削除すると検索・件数の対象が曖昧 | Keep | 集約範囲の規則 |
| Human-created / Unknown role | テレメトリーに証明がない | Reject | 推論を事実としない |
| 名前の観測 / Native Conversation | 統合すると名前の変更や欠落で会話の同一性が変わる | Keep separate | 会話は名前がなくても存在し、同じ名前の別会話も存在する |
| 名前の観測 / 一覧ラベル | 統合するとIDへの代替表示と製品名の観測を区別できない | Keep separate | 一覧ラベルは受動的な表示結果。名前の根拠そのものではない |
| 生成結果 / 製品表示名の観測 | 一つの無区別な名前にすると画面への採用を推測してしまう | Keep as evidence distinction | 別の会話・状態管理サービスではなく、取得根拠の意味の違い |
| 実画面との照合結果 / 会話の実行時状態 | 統合すると非テレメトリー入力と永続的な一致保証を持ち込む | Keep outside runtime model | テスト時点の一致は以後の手動変更を保証しない |
| Name Authority / Enrichment Lifecycle | 非テレメトリーの同期や未観測の改名を前提とする | Reject | 名前の観測に必要な根拠は残し、未確認の同期ライフサイクルは作らない |
| Catalog manager / provider factory | 意味の追加がない | Reject | 実装手順であり概念ではない |

- Unitと一覧行は受動的な読み取り結果として扱う。名前の観測にも、製品に改名を要求する振る舞いや独立した会話の同一性は持たせない。
- 名前の選択は同種の観測の集合に対する規則であり、別の管理サービスは作らない。根拠・時刻・名前の分離を除去すると同時刻の競合や未観測の改名を説明できないため、初期minimality確認を通過する。
- Evidence packet v4はv2の「名前を取得しない」という前提を変更するmaterial revisionである。v2に対する合格を流用しない。独立agent title_scenarios_v4は要件と実観測だけからシナリオを作成し、初期minimality確認後に提示する。適用結果はdesignに記録する。

---

## 4. Main Spec Conceptual Model Replacements

### `session-catalog`

```markdown
### Native Conversation

Native Conversationは一つのテレメトリーsource内の会話である。同一性はsourceとnative IDの組であり、異なるsourceの同じIDは別会話となる。Codexはconversation.id、Claude Codeはsession.idに対応する。Claudeのagent_idとparent_agent_idは会話内のagent関係であり、会話の同一性ではない。

### 名前の観測

名前の観測は、テレメトリーが伝える名前の値と、その根拠である。一つの観測の対象は一つのNative Conversationであり、一つの会話にはゼロ個以上の観測が対応する。名前の一致は会話の同一性を意味しない。

| 要素 | 意味 |
|---|---|
| 対象会話 | 名前が指すsourceとnative IDの組。イベントを実行した会話と対象会話は同じとは限らない |
| 名前の値 | 取得根拠が伝える名前の文字列。会話を識別するキーではない |
| 取得元 | 名前の意味と対象会話への対応を裏付けるテレメトリーの根拠。任意の本文に名前らしい文字列があることとは区別する |
| 観測時刻 | 取得元が名前の値を観測した時点。Agentmetryへの受信時刻とは異なる。根拠から時点を特定できない場合は不明である |

取得根拠は、それが何を示すかで区別する。生成結果の観測は製品が名前を生成したことを示す。製品表示名の観測は、その取得元・時点における表示名を示す。生成結果だけでは製品画面への採用を証明せず、表示名の観測でも後の未観測の改名は証明しない。

### 一覧ラベル

一覧ラベルは行を読むための表示値である。名前を使うラベルは、その行が識別するNative Conversationの名前の観測に対応する。IDによる代替ラベルは名前の観測ではない。親子の集約と名前の帰属は別であり、子の名前は親の名前を意味しない。

名前の有無、値の変更、実画面との一致は、それぞれ別の事実である。実画面との照合結果は対象会話・製品・バージョン・画面・時点を伴う受入テストの証拠であり、Agentmetryが実行時に取得する会話状態ではない。

### Concept: session-link-evidence

Session Link Evidenceは同一source内で観測した親会話から子会話への有向関係である。Projected Membershipはその根拠から解決した親とルートの関係であり、一つの会話は高々一つの親を持つ。成分は同じルートに属する会話の集合である。

RoleはProjected Membershipに基づく分類である。ROOTは解決済みの親がない会話、CHILDは解決済みの親がある会話を意味する。ROOTは人間が作成した証拠ではない。

### Session List View and Unit

| View | Unit |
|---|---|
| ROOTS | 成分全体。行の識別子はルート |
| ALL | 一つのNative Conversation。行の識別子はその会話 |

一覧の活動数・agent数・開始終了時刻と条件判定の対象はUnitである。会話活動は一覧対象のsemantic trace/log activityを指し、関係情報だけでは活動にならない。名前は活動の集約単位や会話の同一性を変更しない。

~~~mermaid
flowchart LR
    E[Session Link Evidence] --> M[Projected Membership]
    M --> R[ROOTS: component]
    M --> A[ALL: Native Conversation]
    N[Native Conversation activity] --> R
    N --> A
    O[名前の観測] -->|対象会話| C[Native Conversation]
    R -->|行の会話| C
    A -->|行の会話| C
    O -->|名前の根拠| L[一覧ラベル]
    C -->|IDによる代替| L
~~~
```

---

## 5. Requirement Candidates

| Requirement slug | Actor and event | Guarantee | Concepts used | Normative representations | Scenario tags |
|---|---|---|---|---|---|
| provider-native-session-identity | exporter→list caller | source修飾の会話単位を維持 | Native Conversation | invariant | happy, compatibility |
| evidence-backed-session-role | caller→list | 解決済み関係から役割を返す | Evidence, Membership | decision | happy, boundary |
| explicit-session-list-views | caller→query | 選んだ単位で検索・集計・ページング | View, Unit | decision | happy, boundary |
| telemetry-only-session-labels | exporter→list caller | 対象会話の生成名を選択。最新候補の競合時はID表示 | Native Conversation、名前の観測、一覧ラベル | decision, invariant | happy, boundary, concurrency, idempotency, compatibility |
| session-list-view-negotiation | caller→API | 既定値と適用表示を明示 | View | partition | happy, error, compatibility |
| session-list-presentation | user→UI | 表示切替・履歴・最新要求と行の整合性、名前の取得範囲を説明 | View, Unit、一覧ラベル | prose | happy, boundary, concurrency, compatibility |
| codex-event-normalization | exporter→ingest | 互換イベントの親子条件を維持 | Evidence | decision | happy, boundary, idempotency |

---

## 6. Unresolved Decisions

| ID | 状態 | 判断・残る証拠 |
|---|---|---|
| D-1 | PR対象の規則を確定 | 同種の根拠で時刻が比較可能なら最新。最新時刻と時刻不明の候補が異なる名前ならID。同名の重複は結果に影響しない。異種の取得元を混合しない |
| D-2 | PR対象の表示を確定 | Claude自動生成名として表示し、IDを併記する。取得元・観測時刻を返す。現在の製品画面との一致を保証しない |
| D-3 | 未解決・PR対象外 | Codexの全件・継続取得。現在の実装はCodexの名前を抽出しない。要望は未完了で維持 |
| D-4 | 未解決・PR対象外 | 手動変更後の名前の追跡。生成名を同期済み現在名としない |
| D-5 | 受入検証未完了 | 実画面と同一会話のテレメトリーを対応付けたfixture。構造を匿名化した実データ由来テストは代替証明にならない |

- 2026-09-08の「PRまで進めて」を、直前の選択規則と承認済みモデルに基づく文書・実装・レビュー・draft PR作成への指示として扱う。文書ごとの停止は行わない。
- D-3〜D-5は名前表示要望全体の完了・公開を妨げる残課題であり、確定したClaude生成名の規則を未決にしない。このchangeはarchiveせず、draft PRに制限を明記する。

---

## 7. Sources

- Evidence packet v4: 2026-09-06の再調査結果、proposal・モデル修正承認、2026-09-08のPR作成指示。独立シナリオには実観測・要件・制約だけを渡す。
- Claude Code: ローカルAgentmetryの直近10万件のClaudeログの調査では、`query_source=generate_session_title` の `assistant_response` に有効なJSONの `title` が45会話分存在する。全45件でnative session IDと保存会話ID、対応するAPIリクエストを照合済み。これは生成応答の取得証拠であり、実画面との一致証拠ではない。
- Codex: 受信済みの `mcp__codex_app.list_threads` の成功ツール結果から、完結したID・titleの組を11会話に対応付け済み。結果には送信時点の省略があり、全件性や現在名を保証しない。調査用のAPI呼び出しによる補完はしない。
- 上記は調査時点の限定sampleであり、全製品版への保証ではない。evidence/provider-title-correlation.mdに残るpacket v2の記録を、名前不存在の根拠として使用しない。個人のID・本文・タイトルは本書に記載しない。
- [既存一覧](https://github.com/kotokumu/agentmetry/blob/main/internal/storage/sqlite/api.go)、[既存関係解決](https://github.com/kotokumu/agentmetry/blob/main/internal/storage/sqlite/session_graph.go)。raw保持は復元境界であり、表示名が存在する根拠ではない。
