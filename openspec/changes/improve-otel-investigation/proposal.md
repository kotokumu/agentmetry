## Why

agentmetry の受信済み OTel データから失敗の根拠や入力本文を調べる際、対象処理への移動と本文の読み取りに負担がある。Web と MCP が返す診断の差も埋め、どちらからでも同じ根拠を確認できるようにする。

---

## What Changes

現行は会話一覧・検索・状態を保持する往復・waterfall・手戻り分析を持つ。根拠リンクは trace 全体を開き、エピソード表示は上位 3 件に限られる。Web の正規化比較と MCP の比較出力には差がある。

次の 6 作業群を計画する。

1. 根拠となる span と本文へ直接移動し、全エピソードを閲覧する。
2. Web/MCP の診断比較について、指標・分母・欠測・分析範囲の契約を揃える。
3. 会話内の目的別表示と隣接詳細で、受信した入力・コンテキスト・応答・ツール内容を読む。
4. 失敗・所要時間・モデル・ツールの構造化フィルタと、ローカルの保存条件を追加する。
5. 未報告・秘匿・表示上の非表示・未投影を区別し、保存済み OTel の追加利用可能性を確認する。
6. 長い trace の全体概要・拡大・種別/エラー絞り込みを追加する。

コンテキストは AGENTS.md・Skill・参照ファイル等を含み得る。参照先だけの記録、読み込んだ本文、モデル入力に含まれた記録を区別する。各送信元で何が届くかは作業群 5 で確認し、未確認の内容の提供を保証しない。

---

## Capabilities

### New Capabilities

- `telemetry-investigation`: 根拠への移動、受信本文の閲覧、構造化フィルタ・保存条件、長い trace の探索に関する製品動作。
- `telemetry-diagnostics`: Web/MCP の比較一致、診断の欠測・分析範囲と、コンテキストの証拠の強さに関する製品動作。

### Modified Capabilities

なし。`otlp-ingestion` の受信・raw 保持・既存正規化契約は維持する。作業群 5 の調査で span event 等の canonical 投影変更が必要と分かった場合、その具体的な mapping と既存要件の変更を別 change として計画する。本 change は未確定の投影方式を先に実装しない。

---

## Non-goals

- Git・リポジトリ・AGENTS.md・Skill・transcript を agentmetry が別途読み出す収集経路。
- プロンプトや設定の編集・版管理・配信、fingerprint 管理の拡張。
- 成果評価、採点、コメント入力管理、実験 runner、外部評価基盤との連携。
- 未受信の内容・実行関係・成果達成の推定。
- 既存 fingerprint 機能の削除や、上流 Claude Code/Codex の wire format の変更。

---

## Impact

- Web：会話詳細、手戻り一覧、navigation、実行表、waterfall、filter とその状態。
- Go：既存 query を基点とする比較・検索・trace 範囲取得。HTTP と read-only MCP のアダプタ。
- API：既存 field・tool 引数を保持する追加的な拡張。query の意味を Web 固有の計算から共有契約へ移す。
- ストレージ：検索に必要な既存 projection の集計・index は必要性を確認して追加する。raw の保持形式と受信経路は変えない。
- 検証：既存の Go/Vitest テスト、Web build、対象操作のブラウザ確認、strict OpenSpec validation。実エージェントの有料 E2E はこの計画の必須条件にしない。

現行実装の根拠：[手戻り表示](https://github.com/kotokumu/agentmetry/blob/main/web/src/components/rework-summary.ts)、[navigation](https://github.com/kotokumu/agentmetry/blob/main/web/src/app/navigation.ts)、[Web 比較](https://github.com/kotokumu/agentmetry/blob/main/web/src/model/rework-comparison.ts)、[MCP](https://github.com/kotokumu/agentmetry/blob/main/internal/transport/mcpserver/server.go)、[受信・raw 保持](https://github.com/kotokumu/agentmetry/blob/main/internal/ingest/otel/receiver.go)。
