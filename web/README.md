# フロントエンドの検証

```sh
npm ci
npm run lint
npm test
npm run build
```

## 録音・読み上げ・保存のブラウザ回帰テスト

`npm run test:browser` は、独立した Chromium・ローカル Vite・モックのマイク、音声プレイヤー、API で実際の React UI を操作する。実マイク、外部 API、既存ブラウザのプロファイルは使用しない。Vite の仮想ページ・モジュールを使い、試験用ページのファイルを作成・削除しない。依存のキャッシュは `web/.vite-recording-test/` に分ける。

Playwright は通常のビルド依存に含めない。既存のインストールがあれば `PLAYWRIGHT_MODULE` にそのモジュールを指定して使える。新しく用意する場合は次のように隔離したディレクトリを使う。

```sh
npm install --prefix /tmp/ai-ni-oshieru-browser-tools --no-save playwright@1.65.0-alpha-2026-10-30
/tmp/ai-ni-oshieru-browser-tools/node_modules/.bin/playwright install chromium
PLAYWRIGHT_MODULE=/tmp/ai-ni-oshieru-browser-tools/node_modules/playwright/index.mjs npm run test:browser
```

既存の Chromium を使用するときだけ `PLAYWRIGHT_CHROMIUM_EXECUTABLE` に実行ファイルの絶対パスを指定する。2026-11-09 の監査では Playwright `1.65.0-alpha-2026-10-30` と Chromium headless shell revision `1248` で成功した。通常は Playwright がインストールする対応版を使用する。

確認する内容:

- 取消直後の再録音と古い `onstop`、チャンク、エラーの隔離
- マイク権限待ちの取消・画面離脱・再試行、同一 tick の連打
- 録音エラー、開始失敗、マイク拒否、画面離脱時のトラック解放
- 30秒の停止要求、最後のチャンク待機、空音声、5 MiB の境界
- 古い文字起こしの抑止、下書き保持、追加・置換、利用者の明示送信
- App の文字起こし通信取消・再試行（API はモック）
- TTS の停止・再生し直し・同文の別ターン、遅延応答の無視、自動再生拒否、音声 API の各障害
- 第2段階の署名更新、教材・回答下書きの復元、保存結果不明時の凍結・復元・同一内容再送、閲覧キーでの再閲覧
- 固定期限到達時の録音解放と送信・保存停止、Storage 拒否時の警告と対話継続
- localStorage は未完了学習と教材下書きだけを期限付きで保持し、保存成功・期限切れで取り除くこと。音声・未確認の文字起こし・保存済み結果・閲覧キーは永続化しないこと
- sessionStorage、IndexedDB、Cookie に学習データを保存しないこと

実マイク・実コーデックの録音品質、スピーカーからの再生、実 Firestore との結合、Speech-to-Text / Text-to-Speech の実 API 疎通はこのテストの対象外。
