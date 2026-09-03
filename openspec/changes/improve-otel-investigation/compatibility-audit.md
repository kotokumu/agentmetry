# 既存契約の確認

対象は baseline `ed6e5038d82d67298c8a569ad67f955caadd5cc9`。2026-09-04 にコードと既存テストを確認した。新機能の実装前の記録であり、新機能の合格を示すものではない。

| 契約 | 現行入力・出力と制限 | 今回の変更 / 維持 | 回帰確認の入口 |
| --- | --- | --- | --- |
| Web Before/After | 同一 source、別会話、妥当な開始/終了、baseline 終了が current 開始以前。初回検証成功 proxy、手戻り token 比率、retry effort 比率、tool failure 比率、検証 100 回当たり recurring loop。分子・分母・欠測理由を保持 | 計算を共有 query へ移す。共有値・差は丸めず、UI の小数 1 桁と方向表示、harness 表示を維持 | [web/src/model/rework-comparison.test.ts](https://github.com/kotokumu/agentmetry/blob/main/web/src/model/rework-comparison.test.ts): all five normalized metrics、invalid/missing measurements、invalid identities/times、one-decimal direction、snapshot tokens、harness relationships |
| MCP `compare_runs` | 明示した 1〜10 件。異なる source も可。既定は wallDuration/activityCount/agentCount/totalTokens/costUsd。要求された集計を実行ごとに返す。baseline の役割や時間的適格性なし | 維持。新規 `compare_rework` に正規化した 2 会話の比較を分離 | [internal/transport/mcpserver/server.go](https://github.com/kotokumu/agentmetry/blob/main/internal/transport/mcpserver/server.go) の `compareRuns`。専用の互換性テストを追加する必要あり |
| MCP 本文 | `includeContent` 既定 false。省略時 not_returned、明示取得時に本文あり available、空なら unavailable | 比較には本文を入れない。明示的 activity 取得を維持 | `TestGetRunTimelineHidesContentByDefault` |
| Activity page | Page 既定/上限 100。offset は非負。continuation は identity/direction に拘束 | 維持。anchor 取得でも上限内 | `TestGetSessionActivitiesIsBoundedAndUsesOpaqueContinuation`、`TestGetRunTimelineBindsContinuationToRunIdentity`、`TestGetRunTimelineRejectsUnsupportedDirection` |
| 本文長 | 現行 MCP `mapActivityWithContent` は明示要求時に保存済み content 全文を返す。独立した文字数上限はない | 存在しない制限を仕様として追加しない。ページ上限と本文取得の opt-in を引き継ぐ | [internal/transport/mcpserver/server.go](https://github.com/kotokumu/agentmetry/blob/main/internal/transport/mcpserver/server.go) の `mapActivityWithContent` |
| 既存 MCP 分析 | `loadRunAnalysis` は 100 件ずつ最大 1,000 activities。coverage は制限を説明 | 既存挙動を維持。新比較は SQLite の一つの read transaction 内で両者の全 projection を分析する | `TestAnalyzeReworkReturnsSessionMetricsAndUnsupportedCapabilities` |
| SessionRework | canonical root と子会話を統合。全保存済み projection、同じ読み取り内の token と harness を返す | 共有比較でもこの意味を利用 | `TestGetSessionReworkAnalyzesTheCompleteStoredSession`、`TestGetSessionReworkAggregatesChildActivitiesIntoCanonicalRoot` |
| 会話 anchor | source-qualified identity、trace/span の両指定。Page.OffsetAround で対象を含む小さい範囲を取得 | 維持 | `TestListSessionActivitiesKeepsAnchorInsideSmallPage`、navigation の `carries a trace span target alongside list filters` |
| Trace | trace ID は 32 桁、span ID は 16 桁の非ゼロ hex。全体時刻/件数/親欠損と詳細 page を返す | 任意 span anchor を追加。未検出は別 span で代替しない | `TestGetTraceCorrelatesConversationsAndReportsIncompleteParents`、`TestGetTraceReturnsNotFoundForUnknownIdentity`、`TestGetTraceRejectsInvalidOTLPIdentity` |
| Trace Web 保持 | controller は最大 2,000 activities を保持。表示窓の件数は query 上限とは別 | 詳細の読み取り範囲と全体概要を区別。live 更新で選択を追い出さない | [web/src/controllers/trace-controller.ts](https://github.com/kotokumu/agentmetry/blob/main/web/src/controllers/trace-controller.ts)。対象外 page/live の行動テストを追加 |
| URL/履歴 | range 1h/24h/7d、source、q を継承。origin は安全な同一 origin path。agent と非負 scrollY を保持 | span、purpose view、構造化条件を追加。従来 URL/戻る動作を維持 | [web/src/app/navigation.test.ts](https://github.com/kotokumu/agentmetry/blob/main/web/src/app/navigation.test.ts)、[web/src/app/agentmetry-app.test.ts](https://github.com/kotokumu/agentmetry/blob/main/web/src/app/agentmetry-app.test.ts) |

## Baseline 検証

以下はすべて成功した。

- `GOCACHE=/tmp/agentmetry-ui-analysis-gocache go test ./internal/query ./internal/transport/mcpserver ./internal/storage/sqlite`
- `npm --prefix web test -- src/app/navigation.test.ts src/model/rework-comparison.test.ts src/app/agentmetry-app.test.ts` — 3 files、39 tests。

新規の anchor、共有比較、filter、overview のテストは各作業の Red/Green として追加し、最後に全体の回帰検証を行う。
