## Purpose

Agentmetryは、テレメトリーで観測された会話の同一性と親子関係を保ち、利用者がルートにまとめた一覧と個々の会話の一覧を切り替えられることを保証する。

## ADDED Requirements

### Requirement: provider-native-session-identity

Agentmetry MUST sourceとnative IDの組で会話を識別する。

- **Behavior Rules**: Codexのconversation.idとClaude Codeのsession.idをそれぞれ会話IDとして扱う。Claudeのagent_id、parent_agent_id、agent.nameは別会話の根拠にしない。対象外sourceも同じsource修飾を維持する。
- **Invariants**:
  - Agentmetry MUST 異なるsourceの同じIDを統合しない。
  - Agentmetry MUST テレメトリーにない合成会話をagentから作らない。
- **Side Effects**: 詳細とダッシュボードの既存の親子集約は変えない。
- **References**: [code] [Provider normalization](https://github.com/kotokumu/agentmetry/tree/main/internal/source)

#### Scenario: Claude agents share one conversation [happy]

- **GIVEN** 利用者のClaude会話Sにrootとsubagentの活動が存在する
- **WHEN** 利用者が全件一覧を取得する
- **THEN** ClaudeのSは一行であり、agent別の合成会話はない

#### Scenario: Identical IDs across providers [compatibility]

- **GIVEN** 利用者のCodexとClaudeに同じIDの活動が存在する
- **WHEN** 利用者が一覧を取得する
- **THEN** 二つのsource修飾された行を取得する

### Requirement: evidence-backed-session-role

Agentmetry MUST 解決済みのProjected Membershipに基づいて各一覧行のROOTまたはCHILDと、同じsource内のルートID・親IDを返す。

- **Behavior Rules**:

  | Rule | Condition: 解決済みの親 | Output or response | Side Effects |
  |---|---|---|---|
  | R1 | なし | ROOT、自分をルート、親なし | なし |
  | R2 | あり | CHILD、解決済みのルートと直接親 | なし |

  空ID・自己参照の根拠は親を作らない。重複は同じ関係となる。複数の異なる親を持つ会話は親なしとなる。循環に属する会話は親なしとなる。親情報の欠落を人間作成の証明とはしない。
- **Concurrency and Idempotency**: 一覧内の関係と活動は一つの読み取りsnapshotに整合する。追加関係の反映は次の一覧取得で行う。
- **References**: [related] [[concept:session-catalog/session-link-evidence]]

#### Scenario: Projected child [happy]

- **GIVEN** callerがsource内のR→C→Gという解決済み関係を持つ
- **WHEN** callerが全件一覧を取得する
- **THEN** Gの役割はCHILD、直接親はC、ルートはRとなる

#### Scenario: Conflicting parents [boundary]

- **GIVEN** callerの会話Cに異なる親PとQの根拠が存在する
- **WHEN** callerが一覧を取得する
- **THEN** CはROOTとなり、人間作成とは説明されない

### Requirement: explicit-session-list-views

Agentmetry MUST 選択したViewのUnitを検索・集計・並び替え・ページングの対象とする。

- **Input and Acceptance**: 既存の時刻・source・検索語・構造化条件・ページサイズ／tokenの受入契約を維持する。
- **Behavior Rules**:

  | Rule | Condition: View | Output or response | Side Effects |
  |---|---|---|---|
  | V1 | ROOTS | 活動がある成分につきルート一行。ルート自身に活動がなくても掲載 | なし |
  | V2 | ALL | 活動があるNative Conversationごとに一行。関係しかない会話は掲載しない | なし |

  Unit全体の活動数とagent数を合計し、開始の最小値・終了の最大値を使う。終了時刻で時間範囲を判定する。検索と構造化条件はUnit全体で判定し、合致したUnit全体の値を返す。検索対象は既存の会話ID・source・イベント名・本文・tool・agent識別子／定義／type・target・model・traceである。ROOTSの子ID指定はそのルートを返し、ALLのID指定はその会話だけを返す。順序は終了時刻降順、source昇順、行ID昇順とし、条件適用後にページ分割する。
- **Invariants**:
  - 各レスポンス MUST sourceと行IDの組が重複しない。
  - ALLの単独行 MUST 子孫の活動を加算しない。
- **Concurrency and Idempotency**: ページtokenは既存のoffset方式であり、別ページ取得間のsnapshot固定は保証しない。
- **Failure Handling**: 取得失敗時は部分的な成功レスポンスを返さない。

#### Scenario: Descendant-only search [happy]

- **GIVEN** 利用者の親Rに一件、子Cに検索語に一致する二件の活動がある
- **WHEN** 利用者が同じ検索でROOTSとALLを取得する
- **THEN** ROOTSはRの三件、ALLはCの二件を返す

#### Scenario: Unobserved root [boundary]

- **GIVEN** 利用者がR→Cの関係とCの活動だけを持つ
- **WHEN** 利用者がROOTSとALLを取得する
- **THEN** ROOTSはR、ALLはCを掲載する

### Requirement: telemetry-only-session-labels

Agentmetry MUST セッションラベルとしてnative IDを表示し、確認済みテレメトリーにない製品側の表示名を推測しない。

- **Behavior Rules**: Claude Code/Codexの製品表示名は取得できない旨を利用者に説明する。prompt・response・slug・agent.name・任意の未知属性を製品タイトルに昇格させない。
- **Invariants**:
  - セッション一覧機能 MUST SDK、ローカルセッションファイル、app-server、追加hookなどの非テレメトリー入力を使用しない。
- **Side Effects**: タイトルの取得・生成・保存を行わない。既存のraw保持方式は変更しない。

#### Scenario: Prompt and slug are not a title [happy]

- **GIVEN** 利用者の会話SのOTLPにprompt、slug、agent.nameがある
- **WHEN** 利用者がセッション一覧を開く
- **THEN** 主ラベルはSのnative IDであり、それらの値はタイトルに使われない

### Requirement: session-list-view-negotiation

Agentmetry MUST 一覧の表示指定を検証し、成功時に適用したViewを明示する。

- **Input and Acceptance**:

  | Partition | Condition or range | Acceptance or result |
  |---|---|---|
  | Default | 指定なし、UNSPECIFIED | ROOTSとして受理 |
  | Explicit | ROOTSまたはALL | 指定通り受理 |
  | Invalid | 上記以外 | InvalidArgument |

- **Behavior Rules**: 現行サーバーは適用Viewと行の関係情報を返す。旧クライアントの省略呼び出しは既存のROOTS結果を維持する。新クライアントがALLを要求して適用確認できない場合、ROOTSをALLとして表示しない。関係情報を持たない旧レスポンスのROOTS表示はIDにフォールバックし、役割を断定しない。
- **Side Effects**: 詳細レスポンスには一覧用関係情報を付けない。MCPの既存一覧はROOTSを使う。
- **References**: [api] [Agentmetry protobuf](https://github.com/kotokumu/agentmetry/blob/main/proto/agentmetry/v1/agentmetry.proto)

#### Scenario: Legacy caller [happy]

- **GIVEN** 旧callerがViewを指定しない
- **WHEN** callerが一覧を取得する
- **THEN** ルートにまとめた結果と適用View ROOTSを受け取る

#### Scenario: Unknown view [error]

- **GIVEN** callerが未定義のView数値を指定する
- **WHEN** callerが一覧を要求する
- **THEN** InvalidArgumentとなる

#### Scenario: Old server cannot acknowledge all [compatibility]

- **GIVEN** 利用者がALLを選び、サーバーが適用Viewを返さない
- **WHEN** 一覧レスポンスが届く
- **THEN** 利用不可と表示し、ルート行を全件として表示しない

### Requirement: session-list-presentation

Agentmetry MUST 利用者がルートにまとめる表示と全件表示を切り替え、観測上の役割と取得制限を理解できる一覧を提供する。

- **Input and Acceptance**: URLの単一のview=allだけをALLとする。省略・重複・未知値はROOTSとし、URLはROOTSならviewなし、ALLなら単一のview=allに正規化する。保存フィルターの意味は変更しない。
- **Behavior Rules**: 日本語・英語の切替操作と説明を提供する。操作はキーボードから可能でaccessible nameを持つ。CHILD行には子であることを表示する。ROOTは「親を未観測」の可能性を説明し、人間作成とは断定しない。子の選択は既存のルート集約詳細を開き、一覧の単独行は維持する。全件一覧に詳細からルート行を追加しない。
- **Side Effects**: View・検索・条件・ページサイズが変わると旧一覧とpage tokenを破棄する。追加ページは同じ条件で要求する。履歴・再読込で表示選択を復元する。ライブ更新は現在の条件で先頭ページを再取得する。
- **Concurrency and Idempotency**: 古い要求の成功・失敗・ページ追加は最新状態を上書きしない。同じ追加ページ要求は一回にまとめ、sourceとIDが重複する行を二重追加しない。切断後の応答は状態を更新しない。
- **Failure Handling**: 要求失敗と未対応の表示を利用不可として示す。再試行を可能にし、エラー本文をそのまま表示しない。

#### Scenario: Switch and restore [happy]

- **GIVEN** 利用者が検索条件付きのROOTS一覧を表示している
- **WHEN** 全件表示を選んで再読込する
- **THEN** 同じ検索条件のALL一覧と子の表示を得る

#### Scenario: Older response arrives last [concurrency]

- **GIVEN** 利用者のROOTS取得中にALL取得が始まる
- **WHEN** ALL成功の後にROOTSが成功する
- **THEN** 一覧と適用表示はALLのままである
