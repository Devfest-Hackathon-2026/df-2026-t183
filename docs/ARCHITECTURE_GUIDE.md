# アーキテクチャガイド

## 実装時の配置

```text
.
├── docs/
├── web/
│   └── src/
│       ├── features/learning/    # 教材抽出、対話、振り返り
│       ├── features/history/     # キー入力、記録表示と削除
│       └── lib/api/              # API の入出力検証とタイムアウト
└── backend/
    ├── Dockerfile                 # Cloud Run で ffmpeg / ffprobe を含む実行環境
    ├── go.mod
    ├── cmd/server/               # 設定、クライアント、HTTP 起動
    └── internal/
        ├── httpapi/              # 入力・CORS・HTTP ステータス
        ├── learning/             # 対話、署名、終了時の確定処理
        ├── gemini/               # Vertex AI の呼び出し
        ├── speech/                # 音声検証と Speech-to-Text V2
        ├── synthesis/             # Cloud Text-to-Speech と MP3 検証
        └── store/                # Firestore の記録（第2段階）
```

フロントと API は `yasusudas/ai-ni-oshieru` に置く。Go は `cmd/server` で依存を組み立てる。HTTP 層は入出力を担当し、学習ロジックは HTTP、Gemini SDK、Firestore の詳細を直接扱わない。使わないディレクトリは先に作らない。

## 教材抽出と画面状態

- TXT は UTF-8 の厳密なデコード。PDF は PDF.js のテキスト抽出を第一候補とし、ページ範囲・ファイル容量を検証して、ブラウザのワーカーで読み取る。開始から10秒を超えたらワーカーを停止し、読み取り直せる状態に戻す。PDF.js の legacy ビルドを使い、PDF.js 本体とワーカーの両方で `Promise.withResolvers` を補う。埋め込みスクリプトやリンクを実行しない。日本語の PDF を読むため、`cMapUrl`・`cMapPacked: true`・`standardFontDataUrl` を設定し、文字の対応表（CMap）と標準フォントのデータを自分の配信元（Firebase Hosting）から配る。設定が無いと、日本語の文字が取れなかったり化けたりする。 [PDF.js のテキスト抽出 API](https://mozilla.github.io/pdf.js/api/draft/module-pdfjsLib-PDFPageProxy.html#getTextContent)
- 抽出後は編集可能な教材本文を使い、ファイル本体は学習状態から外す。日本語の抽出結果と、破損・圧縮された PDF の挙動は実装時に検証する。失敗した場合はページを絞るか、本文を貼り付ける。
- 開始・対話は同時に1件だけ送信し、応答が成功したときにだけ確定履歴と署名を一緒に更新する。画面を移動した後に届いた古い応答を反映しない。
- 終了時は送った本文・履歴・署名を凍結する。結果が不明な場合は同じ終了リクエストを再送する。振り返りが保存済みならキーを表示する。再閲覧では、別のキーを試す際に前の記録とキーを画面から外す。
- 未完了の学習（教材・確定会話・署名・回答下書き・自己確認・凍結状態）と開始前の教材下書きだけは、メモリに加えて最長2時間の固定期限付き localStorage に保持し、保存成功・明示破棄・期限切れで取り除く。保存済み結果・閲覧キー・音声・未確認の文字起こしは永続化しない。学習データやキーは IndexedDB・Cookie・URL・分析イベントにも書き込まない。通信結果は形を検証し、HTML や Markdown として描画しない。
- マイク入力は `MediaRecorder` で30秒を目安に録音停止を要求し（API上限は31秒）、音声を作り、APIへバイナリで送る。文字起こし結果は回答欄の下書きとして表示し、利用者の送信操作後にだけ確定履歴へ追加する。録音・変換の終了時にマイクトラックと一時データを解放する。
- Go の音声 API は ffprobe/ffmpeg でコンテナ・コーデック・長さ・全体のデコード可否を確認し、Speech-to-Text V2 に送る。デプロイ時は `backend/Dockerfile` を使い、Cloud Run のサービスアカウントに必要な認識権限を付ける。Gemini API と音声APIは別経路にする。
- 質問の読み上げは、開始・対話で確定した最新のAIの質問だけを `/api/speech/synthesize` に送り、Go が Cloud Text-to-Speech から MP3 を取得する。ブラウザでは音声を一時的に保持し、再生・停止を操作できるようにする。録音開始・画面離脱時は再生と取得を止め、読み上げ失敗時も文字表示と確定履歴を保つ。

## Go、Gemini、Firestore の境界

- 教材・学習者の発言はデータとして扱い、システム指示やツール操作に昇格させない。履歴を Gemini に渡す前に署名を検証する。ブラウザが勝手に作った `model` 発言は受け付けない。
- 署名はサーバー側で、バージョン付きの固定型・固定順の値から計算する。同じ本文と履歴には毎回同じ正規化を適用する。入力の厳密な検証、文字数と型は [API 設計](API_DESIGN.md) に従う。
- Gemini の指示は [API 設計](API_DESIGN.md#gemini-への指示と評価例) の文面を `internal/gemini/` の1か所に置く。応答は質問の長さ、振り返りの構造と発言の引用を検証する。引用が合わない項目だけを除き、形が不正なら失敗にする。失敗したら古い履歴を保持し、保存や署名の更新をしない。
- 期限・キャンセルをネットワーク処理に伝える。Firestore のトランザクションは記録の確定・削除に絞り、Gemini の呼び出しは外で行う。ログには分類・所要時間・HTTP ステータスを残し、本文、履歴、署名、キーは入れない。
