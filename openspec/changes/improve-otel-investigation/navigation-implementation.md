# 根拠移動の実装検証

## 対象と結果

タスク 2.2 の Web 側は、trace URL の span 指定、対象を含む取得、native span の明示選択・展開・focus、対象不在時の戻り導線を実装する。元の会話 entry に evidence control を保存し、会話・agent・filter・scroll とともに復元する。episode 起点では元の activity URL を維持する。

## Red / Green

| 検証 | Red | Green |
| --- | --- | --- |
| URL と表示窓外の選択 | navigation は span query が欠落。250 spans + 同 ID log の component fixture は対象選択なし | span query を保持。native span を含む表示窓で選択・展開・focus。live の同一対象は再 focus しない |
| 対象取得と不在 | controller は先頭 page を返し、要求した本文が見えない | anchor を read に渡し、同 trace の別 target へ移ると古い結果を表示しない。不在時も trace identity を維持 |
| UI からの移動 | app のリンクに span がなく、対象不在の alert が出ない | activity/episode が span 付き URL を開く。対象不在でも元会話のリンクが残る |
| 戻り focus | history が evidence control を保持せず、戻ったリンクに focus がない | source-qualified 会話、agent、filter、scroll を保ち、元の activity/episode リンクへ focus |
| 4 番目の episode | 既存の3件制限は episode-implementation.md に Red を記録 | 5件の fixture で4件目を開き、本文を読み、元の source/agent/filter と4件目リンクへ戻る。partial 表示を維持 |

## 実行記録

- 対象の navigation / components / episode / app / controllers: 5 files、127 tests 成功。
- 追加の第4 episode の app 往復テスト: 1 test 成功。
- `npm --prefix web run build`: 成功。

対象不在を隠す旧 server の返却も client が検出し、別の span を選ばない。全体のブラウザ検証と目的別 view は後続タスクで確認する。

## レビュー指摘の検証

- 異なる trace の同じ span ID への誤復帰を防ぐ。episode focus は trace/span の組で照合し、`preventScroll` で戻り位置を維持する。
- live 読込中に別 trace へ移動した後の paging 停止を解消する。open/disconnect は古い live loading 状態を解除する。
- native log の trace-only リンクを維持する。明示した関連 trace/span は組で渡し、native trace ID と別の関連 span ID を混ぜない。

各ケースのテストが修正前に失敗し、修正後は components / episode / controllers / app の4 files、122 tests が成功する。build も成功する。
