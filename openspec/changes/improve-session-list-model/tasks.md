## 1. Evidence and Design

- [x] 1.1 `[[session-catalog/telemetry-only-session-labels]]` テレメトリー限定の一次資料とローカルAgentmetryの属性形状を確認し、機密を含まない証拠と取得制限を記録する。
- [x] 1.2 `[change]` quality-spec形式でモデル・仕様・設計を改訂し、独立シナリオと設計レビューのblocking findingを解消する。

---

## 2. Backend

- [x] 2.1 `[[otlp-ingestion/codex-event-normalization]]` `[[session-catalog/provider-native-session-identity]]` provider由来のテレメトリー形状を使い、Codex sent spawn限定・非spawn・重複・Claude同会話のbaselineテストを検証する。
- [x] 2.2 `[[session-catalog/evidence-backed-session-role]]` `[[session-catalog/explicit-session-list-views]]` query Viewと一覧read projection、SQLiteのROOTS/ALLをTDDで実装し、source同ID、深い親子、親未活動、競合／循環、singleton集計を検証する。
- [x] 2.3 `[[session-catalog/explicit-session-list-views]]` 検索・構造化条件・ID指定・paginationが同じUnitに適用されることをテストし実装する。代表query planと既存ROOTS回帰を確認する。
- [x] 2.4 `[[session-catalog/session-list-view-negotiation]]` additive protoを生成し、View検証・適用View・list専用metadata・旧caller・MCP明示ROOTS・detail不変をテストし実装する。

---

## 3. Web

- [x] 3.1 `[[session-catalog/session-list-view-negotiation]]` `[[session-catalog/telemetry-only-session-labels]]` 純粋な一覧mapperとpage readerを実装し、旧server／未知metadata／ALL不適合／ID fallbackをテストする。
- [x] 3.2 `[[session-catalog/session-list-presentation]]` 専用list controllerをTDDで実装し、query切替・page coalesce・重複・逆順応答・refresh・disconnect・live更新を検証する。
- [x] 3.3 `[[session-catalog/session-list-presentation]]` URLと日英の切替・説明・子表示・ページ追加を実装し、keyboard／履歴復元／invalid URL／保存filter独立／子の集約詳細と一覧分離をテストする。

---

## 4. Verification and Delivery

- [x] 4.1 `[[session-catalog/telemetry-only-session-labels]]` source制限の運用文書を更新し、非テレメトリー依存がないことをauditする。
- [x] 4.2 `[change]` format、generated差分、Go tests、対象race、Web tests/build、OpenSpec strict validationを実行し結果を記録する。
- [x] 4.3 `[change]` 独立のモデル／責務／境界／interface／test／実装品質レビューを行いblocking findingを解消する。
- [x] 4.4 `[change]` Conventional CommitとPRを作成し、checks成功を確認してmainへマージする。release tagは作らない。

完了記録: [PR #55](https://github.com/kotokumu/agentmetry/pull/55)は全対象CI成功後、2026-09-06にmainへマージ済み。merge commit: `431eb8011af15552c3ec5f7b847f0ddd8bc6e349`。
