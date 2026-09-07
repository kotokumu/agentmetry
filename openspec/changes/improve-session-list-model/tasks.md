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

---

## 5. Claude Generated Names — Draft PR

- [x] 5.1 `[change]` packet v4・モデル・仕様・設計を整合させ、独立シナリオと構築前レビューのP0/P1を解消する。
- [x] 5.2 `[[session-catalog/telemetry-only-session-labels]]` 匿名化した実観測形状でClaude生成名抽出をTDD実装し、通常応答・無効JSON・redaction・ID矛盾・時刻不明を検証する。
- [x] 5.3 `[[session-catalog/telemetry-only-session-labels]]` 最新・競合・重複・逆順・未知時刻の名前選択をTDD実装する。
- [x] 5.4 `[[session-catalog/telemetry-only-session-labels]]` 既存保存データのpage-scoped読取を実装し、ROOTS/ALL・source同ID・親子・旧データ・read query planを検証する。
- [x] 5.5 `[[session-catalog/session-list-presentation]]` optional API metadata・安全な名前表示・由来/ID/時刻・日英説明をTDD実装し、旧peerと未知metadataを検証する。
- [x] 5.6 `[change]` Go tests、対象race、Web tests/build、buf lint/breaking、OpenSpec strict、diff check、独立実装レビューを実施し記録する。
- [x] 5.7 `[change]` 対象変更をcommitし、残課題を明記したdraft PRを作成する。マージ・archive・release tag作成は行わない。

提出記録: [Draft PR #59](https://github.com/kotokumu/agentmetry/pull/59)。Claude生成名の実装・検証を提出済み。6.1とCodex/手動改名の未解決事項はPR本文にも明記した。

---

## 6. Remaining Acceptance Evidence

- [ ] 6.1 `[[session-catalog/telemetry-only-session-labels]]` 同一Claude会話の実画面表示を正とするfixtureで一致を証明する。生成応答の構造テストでは完了にしない（proposal SC-7）。

Codex全セッション名取得と手動改名追跡はmodel D-3/D-4の未解決要望である。実装方針を未確定のままタスク化しない。5章完了でも名前表示要望全体は未完了である。
