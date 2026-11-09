# Teach Back（日本語名：AIに教える）仕様書

## 読む順番

| 文書 | ここで決めること |
| --- | --- |
| [PRD](PRD.md) | 対象と課題、MVP の範囲、デモの完了条件 |
| [機能要件](FEATURE_REQUIREMENTS.md) | 画面遷移、段階、Figma との対応、操作と受け入れ条件 |
| [技術要件](TRD.md) | 技術選定と比較した案、公開 API の守り、公開前の確認 |
| [アーキテクチャ全体図](ARCHITECTURE_OVERVIEW.md) | ブラウザ・API・Gemini・Firestore の役割とデータの流れ |
| [アーキテクチャガイド](ARCHITECTURE_GUIDE.md) | コードの配置、責務、状態管理 |
| [API 設計](API_DESIGN.md) | 対話、終了時の保存、閲覧キー、Gemini への指示と評価例 |
| [データ設計](DATABASE_DESIGN.md) | Firestore の保存項目、再送時の処理、期限と削除 |
| [マイグレーション](MIGRATIONS.md) | Firestore の初回準備とスキーマ変更 |
| [コーディング規約](CONVENTIONS.md) | Go・TypeScript・画面・テストの共通ルール |
| [開発ルール](DEVELOPMENT_RULES.md) | 段階・分担・日程、Git とフック、起動・デプロイ、審査向けの動作確認 |
| [音声監査の検証記録](AUDIO_AUDIT_VERIFICATION.md) | 音声修正の検証結果と未検証の環境 |
| [提出リポジトリの案内](SUBMISSION_GUIDE.md) | 公式リポジトリの初期 README。公開範囲、利用制限、秘密情報と提出の注意 |

API ごとの実装要件（処理の流れ、受け入れ条件、敵対的検証の観点）は、MVP の3本と音声の追加機能2本。契約は [API 設計](API_DESIGN.md) を正とする。

| 文書 | 対象 |
| --- | --- |
| [対話開始 API](LEARNING_START_API.md) | `POST /api/start`（Issue #1） |
| [継続対話 API](LEARNING_CHAT_API.md) | `POST /api/chat`（Issue #2） |
| [振り返り API](LEARNING_SUMMARY_API.md) | `POST /api/summary`（Issue #3）。第2段階の保存と再送を含む |
| [第2段階のフロント連携](PHASE2_FRONTEND_HANDOFF.md) | 学習署名、保存・再送、閲覧キーによる再閲覧の接続順序 |
| [音声文字起こし API](SPEECH_TRANSCRIPTION_API.md) | `POST /api/speech/transcribe`。録音後に一括で文字へ変換し、確認後に対話へ送る追加機能 |
| [AI の質問読み上げ API](SPEECH_SYNTHESIS_API.md) | `POST /api/speech/synthesize`。AI の質問を Cloud Text-to-Speech で音声化する追加機能 |

初めて読む場合は [アプリ概要](app-overview.md) からでも全体を把握できます。AI コーディングエージェントは、作業前にリポジトリ直下の `AGENTS.md` を読みます。

## 仕様と実装の区別

- **MVP**：3段階で作る。第1段階は、ログインなしで教材を入力し、対話して振り返る。第2段階で、終了時に教材本文・会話・振り返りをサーバーに保存し、発行した12文字の閲覧キーで後日1件ずつ開けるようにする。第3段階は削除。フロント、Go API、仕様書は非公開の開発リポジトリで開発し、検証済みの版を公開の公式提出リポジトリにも反映する。
- **公開条件**：PDF の抽出品質、保存期限の運用値、請求先、最大インスタンス数と予算アラートは [技術要件](TRD.md#公開前に確認すること) に従って検証する。
- **実装状況**：開始・対話・振り返り API、音声文字起こし API、質問読み上げ API と画面の再生操作を実装。Speech-to-Text・Text-to-Speech の有効化・権限、実音声と実モデルでの確認、Cloud Run への再デプロイは環境で確認する。第2段階の署名・保存・再閲覧とフロント接続を実装。実 Firestore の確認は別途行う。第3段階の削除は未実装。[既存のバックエンド Issue](https://github.com/googleteam183/backend/issues) の状況はチーム側で確認する。

## 文書の更新

- 範囲は [PRD](PRD.md)、画面の振る舞いは [機能要件](FEATURE_REQUIREMENTS.md)、入出力は [API 設計](API_DESIGN.md)、保存項目は [データ設計](DATABASE_DESIGN.md) に集約する。
- 実装が仕様と違う場合は、変更内容を決めて関連する仕様・実装・検証を揃える。
- 既存 Issue #1〜#3 は作業の参照元。追加された API 契約は [API 設計](API_DESIGN.md#エンドポイント) を参照する。再閲覧と削除は Issue #3 の保存とは別の受け入れ条件で扱う。

## 参照した資料

- チームのアプリ概要、画面遷移図（11/6）、開発方針メモ・バックエンド設計・Cloud Run デプロイ手順（11/7）。
- チームの [Issue #1 対話開始](https://github.com/googleteam183/backend/issues/1)、[Issue #2 継続対話](https://github.com/googleteam183/backend/issues/2)、[Issue #3 振り返り・保存](https://github.com/googleteam183/backend/issues/3)。
- DevFest GDGoC Japan Hackathon 2026 のキックオフ資料。締切と提出方法は運営の最新案内も確認する。
