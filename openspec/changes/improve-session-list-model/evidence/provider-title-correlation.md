# Telemetry-only session evidence

## 1. Boundary and Packet

Evidence packet v2（2026-09-06）。入力はAgentmetryが受信するテレメトリーに限定する。SDK・ローカルセッションファイル・app-server・追加hookは利用しない。raw保持は再投影のための仕組みであり、未送信の表示名を補完しない。

---

## 2. Provider Contracts

| Provider | Native conversation | Agent / relationship | Display name |
|---|---|---|---|
| Claude Code | session.id | agent_id / parent_agent_idは会話内agentの軸。rootとsubagentは同じ会話を共有し得る | 公式OTLPで製品表示名と同一の属性を確認できない |
| Codex | conversation.id | 互換入力codex.agent_communicationのsent spawnにsender_thread_id / receiver_thread_idがあれば親子根拠を得る。上流の全件保証ではない | slugと製品表示名の同一性は確認できない |

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
