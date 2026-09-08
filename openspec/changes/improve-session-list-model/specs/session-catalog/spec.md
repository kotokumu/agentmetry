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

Agentmetry MUST 受信済みテレメトリーで対象会話を確認できるClaude Codeのタイトル生成結果とCodexの一覧取得結果の名前を一覧ラベルとして表示し、根拠がない場合はnative IDを表示する。

- **Input and Acceptance**: 名前は空白だけでない文字列とし、受信値を保持する。名前の対象はsource修飾された会話IDであり、欠落・矛盾した対象IDや重複した識別フィールドは受理しない。
  Claude Codeはタイトル生成専用の応答を対象とする。通常の応答、マスク済み・省略済み・壊れた生成応答は名前の根拠として受け入れない。
  Codexは成功したCodexアプリの一覧取得結果から、Codex会話であること・対象ID・名前を確認できる完全な項目だけを受理する。ChatGPTなど対象外の項目、任意のツール本文、未対応の結果形式は名前の根拠にしない。送信側の末尾省略が明示された結果では、構造的に到達できる完全な項目を受理し、不完全な最後の項目は受理しない。壊れた中間部分を読み飛ばして後続の名前を探さない。末尾省略以外の構造不正、重複した一覧・形式識別フィールドはその結果全体を名前の根拠にしない。
- **Behavior Rules**:

  | Rule | Condition: 有効な名前の観測 | Output or response | Side Effects |
  |---|---|---|---|
  | N1 | なし | native ID | なし |
  | N2 | 全観測の時刻が既知で、最新時刻の候補が同じ名前 | 最新の観測の名前 | なし |
  | N3 | 新旧不明の観測を含むが、最新時刻の候補と不明時刻の候補が全て同じ名前 | その名前。時刻不明なら不明のまま | なし |
  | N4 | 最新候補が異なる名前で競合し、新旧を確定できない | native ID | なし |

  同じ受信の重複は表示結果を変えない。新旧の比較に受信順を使わない。Codex一覧結果の名前は項目内の対象会話に帰属し、一覧を取得した会話には帰属させない。観測時刻は一覧取得イベントの時刻を使い、会話の最終更新時刻で代替しない。欠落・不正な時刻は不明とする。ROOTSではルートの名前、ALLでは行の会話の名前だけを使用する。期間・source・ページ条件で実行元の会話が一覧外でも、対象会話の有効な名前を使用する。名前によってsource・会話ID・URL・集計・検索の単位を変更しない。
- **Invariants**:
  - セッション一覧機能 MUST SDK、ローカルセッションファイル、app-server、追加hookなどの非テレメトリー入力を使用しない。
  - 名前の表示 MUST 通常のprompt・response・slug・agent.nameから名前を推測せず、製品側の現在名との一致を未観測のまま保証しない。
- **Side Effects**: 既存の受信データにも同じ解釈を適用する。raw保持と活動・費用の集計は変えない。受信側設定を変更しない。
- **Concurrency and Idempotency**: 一覧と名前は同じ読み取りsnapshotを使う。新しい観測は次回取得で反映し、同じ観測集合なら到着順と重複に依存しない。
- **Failure Handling**: 解釈できない名前は無視し、既存テレメトリーを破棄しない。保存データの取得に失敗した場合は一覧要求を失敗とし、名前がない成功レスポンスに置き換えない。
- **References**: [code] [Codex interpretation](https://github.com/kotokumu/agentmetry/tree/main/internal/source/codex)、[api] [Name metadata](https://github.com/kotokumu/agentmetry/blob/main/proto/agentmetry/v1/agentmetry.proto)

#### Scenario: Prompt and slug are not a title [happy]

- **GIVEN** 利用者の会話SのOTLPにprompt、slug、agent.nameがある
- **WHEN** 利用者がセッション一覧を開く
- **THEN** 主ラベルはSのnative IDであり、それらの値はタイトルに使われない

#### Scenario: Received Claude generated title [happy]

- **GIVEN** 利用者のClaude会話Sに有効なタイトル生成応答の名前Tが存在する
- **WHEN** 利用者が一覧を開く
- **THEN** SのラベルはTであり、自動生成名という取得根拠とSのIDを確認できる

#### Scenario: Late older name [concurrency]

- **GIVEN** 利用者の会話Sに時刻t2の名前Bが存在し、t1はt2より前である
- **WHEN** 時刻t1の名前Aが遅れて届き、一覧を再取得する
- **THEN** Sの名前はBのままである

#### Scenario: Codex name belongs to the listed conversation [happy]

- **GIVEN** 利用者のCodex会話Aの一覧取得結果に、Codex会話Bの完全なIDと名前Tが存在し、A自身の名前は未観測である
- **WHEN** Bを含む一覧ページを取得し、Aはそのページに含まれない
- **THEN** Bは観測名Tと元IDで表示され、Aや同じIDのClaude会話へTを転用しない

#### Scenario: Truncated list retains complete entries [boundary]

- **GIVEN** 利用者の受信済みCodex一覧取得結果に、完全な会話Bの項目と、名前の途中で切れた会話Cの項目があり、末尾省略が明示されている
- **WHEN** BとCの一覧を取得する
- **THEN** Bの観測名を表示し、他に名前の根拠がないCはID表示となる

#### Scenario: Name observation does not create activity [compatibility]

- **GIVEN** 利用者の受信済みCodex一覧取得結果に、活動も親子関係もない会話Bの名前が含まれる
- **WHEN** ROOTSとALLの一覧を取得する
- **THEN** 名前の観測だけを理由にBの一覧行や活動を作らない

#### Scenario: Conflicting latest names [boundary]

- **GIVEN** 利用者の会話Sに同じ最新時刻の異なる名前AとBが存在する
- **WHEN** 利用者が一覧を取得する
- **THEN** Sのnative IDを表示し、AとBの一方を受信順で選ばない

#### Scenario: Duplicate observation [idempotency]

- **GIVEN** 利用者の会話Sの名前Tを示す応答が保存済みである
- **WHEN** 同じ応答を再受信し一覧を再取得する
- **THEN** 名前Tと会話Sの対応は変わらず、会話の行は増えない

#### Scenario: Existing telemetry and unnamed parent [compatibility]

- **GIVEN** 利用者の既存データに親Rと子Cがあり、名前の根拠はCだけに存在する
- **WHEN** ROOTSとALLの一覧を取得する
- **THEN** ROOTSはRのID、ALLはCの名前を表示し、子の名前を親へ転用しない

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
  名前の値は実行可能なHTMLではなく文字列として表示する。名前がある行でもnative IDを確認できる。Claudeは自動生成名、Codexは一覧取得結果で観測した名前と区別する。観測時刻があれば確認でき、手動変更後を含む現在名との一致は未保証と説明する。名前metadataがない旧レスポンス、未知の取得元、sourceと取得元の不一致、不正な名前metadataはID表示を維持する。
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
