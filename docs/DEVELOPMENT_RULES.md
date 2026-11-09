# チーム開発ルール

## リポジトリと Issue

- フロント、Go API、仕様書は非公開の `yasusudas/ai-ni-oshieru` に集約する。公式提出先は公開の `Devfest-Hackathon-2026/df-2026-t183`。両リポジトリを併存させ、検証済みの同じアプリと仕様書をそれぞれの PR 経由で `main` に反映する。
- 既存の [Issue #1〜#3](https://github.com/googleteam183/backend/issues) は参照元。実装は [API 設計](API_DESIGN.md) の契約に揃える。閲覧と削除は保存と分けて追跡する。
- 作業ブランチ・PR は1つの目的に分け、PR を出す場合は対象 Issue、仕様差分、検証結果を書く。秘密情報、個人用の指示ファイル、内部議事録、利用許可のない教材はコミットに含めない。
- AI コーディングエージェントは、作業前にリポジトリ直下の `AGENTS.md` を読む。
- 領域ごとの仕様書（例: `docs/LEARNING_START_API.md`）は `docs/` に1領域1ファイルで置き、`main` に入れるときに [仕様書の目次](README.md) の表に足す。API の契約（フィールド、上限、HTTP ステータスとエラー）は [API 設計](API_DESIGN.md) を正とする。領域の文書に書き写す場合は同じ値にし、食い違いを見つけたら API 設計に合わせる。契約を変えたいときは、先に API 設計を直す。

公式側は別の初期履歴を持つため、初回同期ではアプリの追跡ファイルだけを公式の `main` から作ったブランチへ追加する。非公開側の履歴、未コミットの作業、`.env`、資格情報、ビルド成果物を移さない。元の著作権表示と公式側の秘密情報除外ルールを保持する。運営の初期 README は [提出リポジトリの案内](SUBMISSION_GUIDE.md) に残す。公式側では GitHub Actions / Pages を使えない。

## 段階・分担・日程

範囲は [PRD](PRD.md#mvp-の範囲と段階) の3段階。遅れたら第3段階、第2段階の順に外し、第1段階の中でも遅れたら PDF を外す。

| 役割 | 主な担当 |
| --- | --- |
| リーダー | デザイン、画面の仕上げ、進行と判断 |
| バックエンド担当 | Go API、Gemini への指示と評価例、Firestore、デプロイ |
| 画面担当 | 画面の実装（入力・対話・振り返り・再閲覧） |
| 課題・提出担当 | 課題の根拠、提出ドキュメント、デモ動画の構成 |

| 日 | やること |
| --- | --- |
| 11/8 | 第1段階（直接入力・TXT・PDF、対話、振り返りの表示）を公開 URL で通す。課題の根拠をまとめる。請求先がつながらなければ、この日の夜までに課金済みの別プロジェクトへ切り替える |
| 11/9 昼まで | 第2段階（学習中の署名、終了時の保存、閲覧キー、再閲覧） |
| 11/9 | 画面の仕上げ。余裕があれば第3段階（削除） |
| 11/9 夜 | デモ動画と提出物を作る。完成版を `Devfest-Hackathon-2026/df-2026-t183` に反映する |
| 11/10 | 提出フォームの送信と最終確認だけ。機能は足さない |

## Git とフック

- 作業は `main` の最新から切ったブランチで行う（2026-11-08 決定）。編集の前に、目的に合ったブランチにいることを確かめる。
- ブランチ名は `種類/内容`。種類はコミットの `type` と同じ（例: `docs/learning-start-api`、`add/chat-screen`、`fix/summary-quote`）。人の名前は付けない。内容はローマ字でもよい。
- 作業中は、`main` の最新を自分のブランチへ定期的に取り込む（`git pull origin main`）。食い違いや競合は、AI に原因を調べさせて直す。
- 人によるレビューは必須にしない。マージの前に AI に敵対的検証をさせる。「レビューして」ではなく、「この変更には問題があるはずだ」という前提で、仕様書との食い違い・抜け・壊れる場合を探させる。指摘を直し、フックとテストが通ってから、各自で `main` にマージして push する。他の領域の文書やコードに触れる変更は、マージした後にメンバーへ知らせる。
- push は `git push origin <branch>` を使う。`--force` 系は使わない。
- コミットメッセージは `type: 日本語で変更内容を一文`。`type` は `add`・`fix`・`refactor`・`test`・`docs`・`style`・`perf`・`chore`。生成ツールの表記（`Co-Authored-By` など）は付けない。
- クローンしたら、リポジトリ直下で `./scripts/setup-hooks` を1回実行する。`core.hooksPath` が `.githooks` を向く。
  - `commit-msg`: 生成ツールの表記を含むメッセージを拒否する。
  - `pre-commit`: `.env`（`.env.example` を除く）や鍵ファイルのコミットを止める。`backend/go.mod` があれば `gofmt`・`go vet`・`go test` を、`web/package.json` があれば定義済みの `lint`・`test`・`build` を実行する。どちらもまだ無ければ飛ばす。
- `--no-verify` でフックを飛ばすのは緊急時だけにし、理由をコミットメッセージに書いてメンバーに知らせる。

## ローカルでの確認（実装後）

Go は `backend/`、Vite は `web/` で起動する。Go はコードで `genai.BackendVertexAI` を指定し、試験時は Gemini とストアの代替を使用できるようにする。実サービスへの接続は検証用プロジェクトの ADC を使う。 [ADC の設定](https://docs.cloud.google.com/docs/authentication/set-up-adc-local-dev-environment)

```bash
go test ./...
gcloud auth application-default login
export GOOGLE_CLOUD_PROJECT='your-dev-project'
export GOOGLE_CLOUD_LOCATION='your-vertex-location'
export GEMINI_MODEL='your-available-flash-model'
export GEMINI_MAX_OUTPUT_TOKENS=2048
export SPEECH_MODEL='chirp_2'
export SPEECH_LOCATION='asia-southeast1'
export ALLOWED_ORIGINS='http://localhost:5173'
export PORT=8080
go run ./cmd/server
```

第1段階の API と音声文字起こしの設定例は `backend/.env.example` を参照する。音声 API を使うローカル環境では `ffmpeg` / `ffprobe` と Speech-to-Text・Text-to-Speech API の有効化と権限が必要。`SPEECH_MODEL` と `SPEECH_LOCATION` は起動時に Speech-to-Text クライアントへ渡される。`SPEECH_MODEL` が空欄なら文字起こしを無効化し、`SPEECH_LOCATION` が空欄なら `asia-southeast1` を使う。文字起こしの音声検証は Linux 専用で、macOS では無効になる。これらの変数は Text-to-Speech のモデル・地域設定ではない。第2段階の保存は、対応フロントを用意した検証環境で `SESSION_KEYS_JSON` と `HISTORY_KEYS_JSON` を両方設定すると有効になる。両方が空欄の場合は第1段階の API として動作する。片方だけの設定では起動に失敗する。`FIRESTORE_DATABASE_ID` は既定 `(default)`、`HISTORY_TTL_DAYS` は既定30日。鍵の形式は [バックエンド README](../backend/README.md#設定と起動) を参照し、秘密値はリポジトリに置かない。

ローカルのユーザー ADC で TTS の割り当て先が未設定なら、起動コマンドに `GOOGLE_CLOUD_QUOTA_PROJECT="$GOOGLE_CLOUD_PROJECT"` を付ける。割り当て先の API 有効化状態と呼び出し主体の `serviceusage.services.use` を確認する。Cloud Run へ同じ値を強制せず、実行サービスアカウントと既存の認証設定で別途検証する。

画面は Node.js 22.18 以降を使い、別ターミナルで起動する。`VITE_API_BASE_URL` に公開設定の API の URL を指定する。同じオリジンで `/api/` を配信する場合は空欄にできる。ブラウザへ公開する設定の例は `web/.env.example` にある。

```bash
cd web
npm ci
cp .env.example .env.local
npm run dev -- --strictPort
```

`npm run lint`、`npm test`、`npm run build` で型、ユニットテスト、配信用ビルドを確認する。PDF.js のワーカー・CMap・標準フォントは Web と同じ配信元を使う。`npm run dev` と `npm run build` の前処理で配信用データを用意する。

## デプロイ手順（実装後）

| 環境 | Cloud Run のアクセス | 接続確認 |
| --- | --- | --- |
| IAM 付き検証 | `--no-allow-unauthenticated` | 権限のある開発者が認証付きで API を確認する。別オリジンのブラウザはこの URL に直結しない |
| 審査向け公開 | `--allow-unauthenticated` | Firebase Hosting の画面からログインなしで API を呼ぶ。最大インスタンス数、予算アラート、データ保護、期限が検証済みの場合に開く |

1. 対象プロジェクトの請求先を確かめる。`team183-ai-ni-oshieru` が 11/8 の夜までに請求先につながらなければ、課金済みの別プロジェクトに切り替える。Cloud Billing の予算アラートを設定する。Cloud Run・Cloud Build・Artifact Registry・Vertex AI・Firestore・Secret Manager の設定を確認する。検証環境の Firestore と鍵を審査環境から分ける。実行用サービスアカウントに必要な呼び出し・DB・秘密参照の権限だけを付ける。 [ソースデプロイ](https://docs.cloud.google.com/run/docs/deploying-source-code) · [サービス ID](https://docs.cloud.google.com/run/docs/configuring/services/service-identity)
2. リポジトリ外の `RUN_ENV_FILE` に `GOOGLE_CLOUD_PROJECT`、`GOOGLE_CLOUD_LOCATION`、`GEMINI_MODEL`、`ALLOWED_ORIGINS`、`GEMINI_MAX_OUTPUT_TOKENS` を設定する。出力上限は正の整数とし、未設定・不正なら起動させない。第2段階からは、保存期間の `HISTORY_TTL_DAYS` と、`SESSION_KEYS_JSON`・`HISTORY_KEYS_JSON` を設定する。鍵は用途別に、現在の鍵 ID と検証用の旧鍵を含めた設定を Secret Manager から渡す。Cloud Run の `PORT` で待ち受ける。
3. `backend/` で、対象の `RUN_PROJECT`、`RUN_SERVICE_ACCOUNT`、`RUN_ENV_FILE`、`RUN_MAX_INSTANCES`、`RUN_CONCURRENCY` と、版を固定した秘密の参照 `RUN_SESSION_SECRET_REF`、`RUN_HISTORY_SECRET_REF` を設定して実行する。`RUN_ACCESS` は検証時に `private`、検証を完了した審査環境で `public` を使う。 [Go buildpack](https://docs.cloud.google.com/docs/buildpacks/go) · [gcloud run deploy](https://docs.cloud.google.com/sdk/gcloud/reference/run/deploy)

   ```bash
   case "$RUN_ACCESS" in
     private) run_invoker_flag=--no-allow-unauthenticated ;;
     public) run_invoker_flag=--allow-unauthenticated ;;
     *) exit 2 ;;
   esac

   gcloud run deploy learning-api \
     --project "$RUN_PROJECT" \
     --region asia-northeast1 \
     --source . \
     --set-build-env-vars GOOGLE_BUILDABLE=./cmd/server \
     --service-account "$RUN_SERVICE_ACCOUNT" \
     --env-vars-file "$RUN_ENV_FILE" \
     --update-secrets "SESSION_KEYS_JSON=$RUN_SESSION_SECRET_REF,HISTORY_KEYS_JSON=$RUN_HISTORY_SECRET_REF" \
     --timeout=75s \
     --max "$RUN_MAX_INSTANCES" \
     --concurrency "$RUN_CONCURRENCY" \
     "$run_invoker_flag"
   ```

   第1段階では `--update-secrets` の行を外す。第2段階の鍵を渡すのは、学習署名を送る対応フロントの接続を確認してからにする。

4. Firebase Hosting の公開ドメインを Go の許可オリジンに設定する。リポジトリのルートで、審査用の HTTPS API URL を指定して Web をビルドし、`firebase.json` を使って対象プロジェクトに配信する。第2段階からは Go と同じ保存期間を `VITE_HISTORY_TTL_DAYS` に指定する。

   ```bash
   npm --prefix web ci
   VITE_API_BASE_URL="$RUN_API_URL" npm --prefix web run build
   firebase deploy --project "$RUN_PROJECT" --only hosting
   ```

   ログインしていない実ブラウザで `OPTIONS` と `POST` を検証する。組織のポリシーで匿名公開できない場合は公開済みと判断せず、審査者が使える入口を別途用意する。
5. GitHub への push と配信は別操作。障害時に Cloud Run の旧リビジョンや Hosting の旧版へ戻す前に、現行 API・署名・保存済みキーが使えるか確認する。 [Cloud Run のトラフィック切り替え](https://docs.cloud.google.com/run/docs/rollouts-rollbacks-traffic-migration)

## 確認するシナリオ

| ケース | 期待結果 |
| --- | --- |
| 直接入力・TXT・PDF | 本文を確認して開始し、対話・振り返りまで通る（第2段階からは保存と別ブラウザでの再閲覧まで）。日本語の PDF で文字が化けない。Shift_JIS、スキャン、破損、高圧縮の PDF、1万字超過からは入力し直せる |
| 履歴の偽造 | 発行していない ID、他セッションの署名、`model` 発言・教材の改変で生成も保存も起きない |
| 生成結果 | 形式が正しい質問は300文字まで通る。301文字以上や、答えだけ・質問が複数ある応答は502。[評価例](API_DESIGN.md#gemini-への指示と評価例) の7件を実モデルで通す（質問の連発・教材の命令・答えの先出しをしない）。AI の発言から取った引用や、実際にない引用の項目は振り返りから除かれる。指示文を変えたら評価例を通し直す |
| 音声入力（任意） | マイク拒否・未対応ブラウザでは手入力できる。実音声31秒・5 MiB超過や不一致形式は Speech-to-Text を呼ばず拒否する。文字起こし後に確認・修正せず `/api/chat` が呼ばれない。録音の終了・キャンセル・画面離脱でマイクが解放される |
| 質問の読み上げ（任意） | 開始・対話の質問が文字表示された後に再生される。自動再生拒否時はボタンで再生できる。停止、再生し直し、録音開始、画面離脱、音声化失敗時も履歴・回答欄を保ち、不要な音声を止める |
| 保存競合と通信断 | DB 障害で `saved: true` を返さない。同じ内容の並行保存と応答喪失後の再送は1件・同じキー・同じ日時。署名済みの別内容は409 |
| 閲覧キー（第2段階） | 12文字が4文字ごとに区切って表示される。小文字・ハイフンなし・前後の空白ありでも同じ記録が開ける。1文字違い、ID のみ、失効後は本文を返さない。同じ内容の再送で同じキーが返る |
| 削除（第3段階） | 削除後は同じキーで開けない。削除と終了再送が競合しても復活しない |
| 審査環境の守り | 匿名ブラウザで接続できる。入力上限を超えた要求は Gemini を呼ばずに断る。最大インスタンス数・同時処理数・予算アラートが設定済み。Cloud Run が返す429と、DB 停止時の503で画面が「混雑・一時障害」を出す |
| 画面とデータ | スマートフォン（縦・横）・タブレット・PC の主要フォーム、ログ・URLにキーや学習データが残らないこと、一時保存の固定期限・保存成功・明示破棄での除去、保存済み結果・閲覧キー・音声が永続化されないことを確認する |

文書のみの変更ではリンク、例、図に加え、上のケースを仕様の入出力と状態遷移で照合する。アプリの検証は実装後にリビジョンと結果を記録する。
