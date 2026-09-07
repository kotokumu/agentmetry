# Telemetry-only session evidence

## 1. Boundary and Packet

Evidence packet v4（2026-09-08）。入力はAgentmetryが受信するテレメトリーに限定する。SDK・ローカルセッションファイル・app-server・追加hookは利用しない。raw保持は再投影のための仕組みであり、未送信の表示名を補完しない。

---

## 2. Provider Contracts

| Provider | Native conversation | Agent / relationship | Display name |
|---|---|---|---|
| Claude Code | session.id | agent_id / parent_agent_idは会話内agentの軸。rootとsubagentは同じ会話を共有し得る | 生成専用のassistant_responseにJSON titleを実観測。画面との一致は未検証 |
| Codex | conversation.id | 互換入力codex.agent_communicationのsent spawnにsender_thread_id / receiver_thread_idがあれば親子根拠を得る。上流の全件保証ではない | 受信済みlist_threads出力にid/titleを実観測。省略があり全件保証不可。slugは名前の根拠にしない |

- [Claude monitoring](https://code.claude.com/docs/en/monitoring-usage)
- [Claude observability](https://code.claude.com/docs/en/agent-sdk/observability)
- [Codex OTEL contract](https://github.com/openai/codex/blob/main/codex-rs/otel/README.md)
- [Codex session telemetry](https://github.com/openai/codex/blob/main/codex-rs/otel/src/events/session_telemetry.rs)
- [Agentmetry Codex adapter](https://github.com/kotokumu/agentmetry/blob/main/internal/source/codex/profile.go)
- [Agentmetry Claude adapter](https://github.com/kotokumu/agentmetry/blob/main/internal/source/claude/plugin.go)

Lunaの独立調査は上記の契約とadapterを照合する。送信されるpromptやagent.nameはセッションタイトルの証拠ではない。独自に要約しても製品表示名の再現とはならない。

---

## 3. Verification Status

- 公式契約・既存adapter照合: 完了。
- ローカルAgentmetryのテレメトリー属性形状: 2026-09-06にread-onlyで確認。Codexの1会話の直近500 logsはconversation.idと保存run_idが一致し、slugを持つ。Claudeの1会話の直近500 logsはsession.idとrun_idが一致する。両sampleでsession.name/session_titleは0件。これは限定sampleであり、全バージョン・全属性の不存在を証明しない。
- 同じClaude会話に投影agent IDを2件確認。Codexのsample componentは27会話、直接親あり26件。確認したCodex logsにはcommunication/delegationイベントがなく、sent spawn入力fixtureはadapter互換契約の検証であって今回の実観測ではない。個人のID・本文・タイトルは記録しない。
- テストfixture: provider由来の属性名・関係を残し、IDと内容は匿名化した値を使う。製品表示名との一致を実証済みと主張しない。
- 2026-09-06の追加調査: Claude直近10万logsの45会話でquery_source=generate_session_title、assistant_response.responseのJSON title、session.id/run_id/API requestの対応を確認。Codex受信済みツール結果は11会話分の完結したid/titleを含む。値は記録しない。
- 2026-09-08の再確認: 保存済みClaude応答からservice.name=claude-code、event.name=assistant_response、query_source=generate_session_titleを確認。response/session.id/event.timestamp/request_idは文字列。読み取り専用で確認し、実画面や送信設定には触れない。
- 受信応答の形状テストと実画面を正とする受入テストは別である。後者、Codex全件取得、手動改名追跡は未完了のままdraft PRを作成する。
