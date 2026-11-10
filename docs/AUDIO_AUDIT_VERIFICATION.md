# 音声監査の修正と検証

## 対象

2026-11-09 UTC、開発リポジトリの `main` `6b01dc2c1f22074fb35327bdd1e36af80d5f815c` を基準にした音声・保存連携の追試。PR #9 のマージ後に入ったプロダクト名・Puck 音声の変更を含む。PR #9 作成時の検証結果と、この追試結果は別のものとして扱う。

## 修正内容

- 音声 API の HTTP 400・401・404・413・415・429 とキャンセルを日本語で案内する。キャンセルの例外名 `AbortError` は維持し、本文受信と同時の期限超過も成功にしない。
- 読み上げの停止でリクエスト・Audio・Object URL を解放し、エラーを解除する。停止後の再生は再取得する。古い `play()` の完了やイベントは新しい再生に影響させない。
- MP3 の全フレーム構造検査を ffmpeg のデコード前に行う。固定の合成無音 MP3 と純 Go の判定テストを用意し、ffmpeg がない環境でも正常なフレーム構造・不正応答・デコーダー不在時の拒否を検証する。構造検査だけで成功にはせず、実行時の ffmpeg デコードは維持する。
- 音声付き保存のテストで、実際に保存した記録から発行した閲覧キーを照合する。保存成功応答のキーで再閲覧し、教材・履歴・振り返り・日時を比較する。任意キーと1文字違いのキーは404になる。
- `SPEECH_MODEL` / `SPEECH_LOCATION` は未使用ではなく、起動時に Speech-to-Text クライアントへ渡される。削除せず、有効化条件と Linux 専用の音声検証を設定例・開発ルールに明記した。

## 実施した検証

実行環境は macOS arm64、Go 1.26.10、Node.js 24.18.0。既存の依存関係、ffmpeg、Playwright、Chromium を使用し、ソフトウェアの追加導入はしていない。

| コマンド・確認 | 結果と範囲 |
| --- | --- |
| `gofmt -l .`、`go vet ./...` | 成功 |
| `go test -count=1 -v ./...` | 成功。ただし下記 Linux 専用7テストはスキップ |
| `go test -count=1 -race ./internal/httpapi ./internal/synthesis` | 成功 |
| ffmpeg を含まない PATH で synthesis の要求・不正応答・MP3フレーム・fail-closed テスト5件を実行 | 成功。これらのテストにスキップなし |
| `npm run lint`、`npm test`、`npm run build` | 成功。ユニットテスト20件。既存の PDF バンドルサイズ警告あり |
| `npm run test:browser` | 既存 Chromium で成功。録音・音声再生・HTTP API は代替実装。17シナリオで取消、遅延応答、連打、停止・再取得、自動再生拒否、旧play完了・旧イベント、画面非表示・アンマウント、保存再送・再閲覧、期限・非永続化を確認 |
| `git diff --check` | 成功 |

ffmpeg 不在の追試では PATH から ffmpeg を除外し、`TestClientSendsZeroJapaneseVoiceMP3Request`、`TestClientRejectsMalformedAudio`、`TestMP3FrameChecksWithoutFFmpeg`、`TestMP3SupportedFrameLayoutsWithoutFFmpeg`、`TestClientWithoutMP3DecoderFailsClosed` を実行する。実デコーダーの試験 `TestClientDecodesFixedMP3WithFFmpeg` は、ffmpeg がある通常の Go テストで別に成功を確認する。

固定 MP3 は `ffmpeg -f lavfi -i anullsrc=r=24000:cl=mono -t 0.1 -f mp3` で作った合成無音であり、ユーザーの音声ではない。

ブラウザの初回実行は Phase 2 開始後の画面遷移待ちでタイムアウトした。遷移の明示待機を追加し、試験内の署名期限もブラウザの時計から計算して追試した。macOS のサンドボックス内では Chromium の起動が拒否されたため、承認済みのローカル回帰テストとしてサンドボックス外で実行した。

## 未実施・既知の制約

- Linux 専用の `TestAudioValidatorBoundaries`、`TestAudioValidatorInvalid`、`TestAudioTimestampGaps`、`TestAudioRejectsCompressedTimestamps`、`TestAudioRejectsTruncatedWebM`、`TestAudioRejectsDigitalSilence`、`TestAudioAllowsQuietSignal` は macOS ではスキップ。録音コンテナの実デコード検証全体が成功したとはみなさない。
- 実端末のマイク・スピーカー、実 Cloud Speech-to-Text / Text-to-Speech、公開環境のブラウザ音声、実 Firestore の保存・再閲覧は未検証。Go の保存テストは記録とキーを照合する代替ストア、ブラウザは API 代替を使用する。
- Text-to-Speech エンドポイントの未認証公開は既知の設計として変更していない。今回の追試で API 有効化、IAM、課金設定、デプロイは変更していない。
