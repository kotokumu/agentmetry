## Why

セッション一覧には、プロバイダーの会話単位と親子関係の説明がない。利用者は親子をまとめた一覧と子を含む一覧を切り替えられない。

---

## Intended Outcomes

- 通常は観測された親子関係のルートにまとめ、「すべてを表示」で各会話を確認できる。
- プロバイダーの会話単位を尊重し、親子関係と人間による作成を混同しない。
- 製品側の表示名をテレメトリーから確認できない場合はIDを表示し、その制限を説明する。

---

## Success Criteria

- SC-1: 親R・子Cに活動がある場合、通常一覧はR、全件一覧はRとCを返す。
- SC-2: Claudeの同じsession.id内の複数agent_idを別セッションに増殖させない。
- SC-3: 全件一覧の子を選んでも一覧の集計値や行を集約詳細で置き換えない。
- SC-4: 表示切替、検索、条件、履歴移動、ライブ更新、ページ追加が同じ表示範囲を維持する。
- SC-5: SDK、ローカルセッションファイル、app-server、追加hookに依存せず、表示名を推測しない。

---

## Scope

### In Scope

- テレメトリー由来の会話識別子、解決済み親子関係、ルート／全件の一覧。
- 一覧の表示範囲に沿った検索・条件・ページング、旧APIとの互換性。
- プロバイダー差、取得不能な表示名、親情報欠落の説明と回帰テスト。

### Out of Scope

- テレメトリー以外の入力、独自のタイトル生成、promptやslugを製品の表示名とみなす処理。
- 人が作成したことの保証、未観測の親関係の推測、Claudeのagentを合成セッションに分割する処理。
- 詳細・ダッシュボードの集計単位変更、DBスキーマ変更、タイトル権威管理・収集worker。

---

## Capabilities

### New Capabilities

- `session-catalog`: 利用者がプロバイダー由来の会話を識別し、観測された関係に応じて一覧を切り替える保証。既存の取り込み仕様は一覧を所有しない。

### Modified Capabilities

- `otlp-ingestion`: `[[otlp-ingestion/codex-event-normalization]]` — 既存のCodex互換イベントの親子抽出条件を明確化する。実装動作は維持する。

---

## Affected Concepts

| Concept | Candidate owner capability | Change |
|---|---|---|
| Native Conversation | session-catalog | プロバイダー修飾の同一性を定義 |
| Session Link Evidence / Projected Membership | session-catalog | 観測事実と解決済み関係を区別 |
| Session List View / Unit | session-catalog | ルート集約と単独会話を区別 |

---

## Decisions Required

None. ユーザーの「テレメトリーデータ以外は使えない」「それ前提で進めて」を適用する。送信されない表示名は再現しない。

---

## Impact

- 一覧APIの追加フィールドは旧クライアントの通常一覧を維持する。
- 上流に親情報がなければルート相当の行として残る。人間作成の保証にはならない。
- Claude Code/Codexと同じ表示名を得る要望は、現行の確認済みテレメトリー契約では実現できない。ID表示と説明が提供範囲となる。
