# AGENTS.md

Codex / Claude などの AI コーディングエージェントは、このリポジトリで作業する前に本ファイルを読み、以下のルールに従う。

## 基本方針

- 回答・説明・PR コメントは日本語で書く。
- 作業前に現在のブランチと差分を確認する。
- 編集や実装の前に、作業内容に対応する作業ブランチにいることを確認する。`main`、または目的が異なるブランチにいる場合は、編集前に新規ブランチを作る。
- 未コミット差分の由来や目的が不明な場合は、ブランチ作成や実装の前にユーザーへ確認する。
- 既存コード・docs を優先し、無関係なリファクタリングやフォーマット変更を混ぜない。他者の変更を巻き戻さない。
- 生成ツール名など変更内容と無関係な trailer（`Co-Authored-By` 等）をコミットメッセージへ付けない。
- 普段の開発は `yasusudas/ai-ni-oshieru` で行う。公式提出先は公開の `Devfest-Hackathon-2026/df-2026-t183`。両方を併存させ、検証済みの同じアプリをそれぞれの PR 経由で `main` に反映する。公開先への push と本番デプロイは別の操作として承認範囲を確認する。

## 必ず参照するドキュメント

開発・実装は以下の文書を正とする。変更時は同じブランチで該当文書を更新する。

| 内容 | パス |
| --- | --- |
| 仕様書の目次 | `docs/README.md` |
| アプリ概要（初めて読む場合） | `docs/app-overview.md` |
| プロダクト要件（誰のために・なぜ・MVP の3段階） | `docs/PRD.md` |
| 機能要件（F-01〜F-05・受け入れ条件・Figma との対応） | `docs/FEATURE_REQUIREMENTS.md` |
| 技術要件（技術選定・公開 API の守り・公開前の確認） | `docs/TRD.md` |
| アーキテクチャ全体図 | `docs/ARCHITECTURE_OVERVIEW.md` |
| コード配置と依存方向 | `docs/ARCHITECTURE_GUIDE.md` |
| API 設計（契約・閲覧キー・Gemini への指示と評価例） | `docs/API_DESIGN.md` |
| データ設計（Firestore） | `docs/DATABASE_DESIGN.md` |
| データのマイグレーション | `docs/MIGRATIONS.md` |
| 開発ルール（段階・分担・日程、Git とフック、デプロイ、確認シナリオ） | `docs/DEVELOPMENT_RULES.md` |
| 領域ごとの仕様書（API ごとの実装要件など） | `docs/README.md` の表から辿る |

API の契約は `docs/API_DESIGN.md` を正とする。領域ごとの仕様書と食い違ったら、API 設計に合わせる。

## よく使うコマンド

```bash
./scripts/setup-hooks   # クローン後に1回だけ

# backend/
gofmt -l .
go vet ./...
go test ./...
go run ./cmd/server

# web/
npm ci
npm run dev
npm run lint
npm test
npm run build
```

## Git / ブランチ / コミット

- ブランチ、コミットメッセージ、マージの手順は `docs/DEVELOPMENT_RULES.md` の「Git とフック」に従う。
- ブランチは `main` の最新から切り、名前は `種類/内容`（例: `docs/learning-start-api`、`add/chat-screen`）。人の名前は付けない。
- 作業中は `main` の最新を定期的に取り込み、食い違いを直す。
- `main` へマージする前に、変更に問題があるはずだという前提で敵対的検証を行う。仕様書（特に `docs/API_DESIGN.md`）との食い違い、抜け、壊れる場合を探し、すべて直してからマージする。
- push は `git push -u origin <branch>` または `git push origin <branch>` を使う。`--force` 系は使わない。
- コミットメッセージは `type: 日本語で変更内容を一文` 形式（`add` / `fix` / `refactor` / `test` / `docs` / `style` / `perf` / `chore`）。

## 実装時の注意

- いま作っている段階を `docs/PRD.md` で確かめ、先の段階の機能を前倒しで混ぜない。
- Go の Gemini クライアントはコードで `genai.BackendVertexAI` を明示する。SDK の既定動作や環境変数だけに頼らない。
- Gemini への指示文は `docs/API_DESIGN.md` の文面を `backend/internal/gemini/` の1か所に置く。指示文を変えたら評価例を実モデルで通し直す。
- 秘密値（署名鍵・閲覧キー用の鍵・サービスアカウント）をブラウザへ渡さない。`VITE_*` は全利用者から見える前提で扱う。
- ブラウザから Firestore の SDK を使わない。読み書きは Go API だけが行う。
- 教材本文・会話・振り返り・閲覧キーを、ログ・URL・分析イベントに残さない。第2段階の未完了の学習と開始前の教材下書きだけは、最長2時間の固定期限付き localStorage で保持し、保存成功・明示破棄・期限切れで取り除く。保存済みの振り返り・閲覧キー・音声・未確認の文字起こしはブラウザの永続領域に残さない。テストやドキュメントの例に本物のキーを書かない。
- 環境変数を追加・変更した場合は `docs/DEVELOPMENT_RULES.md` と `.env.example` も更新する。
- 成果物に不要なコメントアウトや説明用コメントを残さない。
- コミット前に、`backend/` は `gofmt`・`go vet`・`go test`、`web/` は `npm run lint`・`npm run test`・`npm run build` を通す（pre-commit フックが同じ内容を実行する）。
