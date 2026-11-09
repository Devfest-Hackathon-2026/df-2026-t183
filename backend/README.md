# 第2段階の学習 API

Go の HTTP API。Vertex AI の Gemini で対話・振り返りを生成します。第2段階では終了時だけ教材・会話・振り返りを Firestore に保存します。第2段階の学習中は、教材と会話に対する署名をブラウザへ返します。任意の音声入力は Google Cloud Speech-to-Text V2 で日本語に文字起こしします。AI の質問は Cloud Text-to-Speech で日本語音声にできます。API 契約は [API 設計](../docs/API_DESIGN.md) を参照してください。

## 必要なもの

- Go 1.26.10 以降。
- Vertex AI が有効な Google Cloud プロジェクト、請求先、利用可能な Gemini モデル。
- 第2段階を使う場合は Firestore が有効な検証プロジェクトと、実行用サービスアカウントに Firestore と用途別の Secret Manager シークレットへの最小限の権限。
- 質問の読み上げには、Speech-to-Text とは別に [Cloud Text-to-Speech API](https://docs.cloud.google.com/text-to-speech/docs/get-started) の有効化と実行主体の呼び出し権限が必要です。MP3 の検証には `ffmpeg` を使います。初期化できない場合も文字の対話・署名・保存は利用でき、読み上げ API だけが設定エラーを返します。
- ローカルで実サービスを使う場合は検証用プロジェクトの ADC。通常の `go test ./...` は認証もネットワークも不要です。
- 音声 API を使う場合は Speech-to-Text API も有効化し、実行主体へ Speech-to-Text 呼び出し権限（[`roles/speech.client`](https://docs.cloud.google.com/iam/docs/roles-permissions/speech)）を付与する。メモリ内ファイルで検証するLinux実行環境と `ffmpeg`・`ffprobe` が必要。macOSなどLinux以外では文字起こしAPIを無効化する。Dockerfile のランタイムイメージには含まれる。ローカルで音声 API を使わない場合は `SPEECH_MODEL` を空欄にする。

依存は Google Gen AI Go SDK v1.77.0（Apache-2.0）、Firestore Go SDK v1.21.0（Apache-2.0）、Unicode 正規化の `golang.org/x/text` v0.43.0（BSD-3-Clause）です。Firestore のトランザクション・ADC・再試行を公式 SDK に委ねるため依存を追加しています。詳細は `go.mod` / `go.sum` を参照してください。

## 設定と起動

`.env.example` は項目の例です。自動読み込みはしません。

| 環境変数 | 設定 |
| --- | --- |
| `GOOGLE_CLOUD_PROJECT` | 必須。Vertex AI と Firestore のプロジェクト |
| `GOOGLE_CLOUD_QUOTA_PROJECT` | ローカルのユーザー ADC で TTS を使う場合、API が有効で利用権限のある割り当て先プロジェクトを指定。未指定なら ADC / SDK の既定値に従う。Cloud Run へ一律には強制しない |
| `GOOGLE_CLOUD_LOCATION` / `GEMINI_MODEL` | 必須。使用可能なモデルと場所 |
| `GEMINI_MAX_OUTPUT_TOKENS` | 必須。正の整数 |
| `ALLOWED_ORIGINS` | 必須。カンマ区切りの許可オリジン |
| `PORT` | 既定値8080 |
| `SPEECH_MODEL` / `SPEECH_LOCATION` | モデルが空欄なら文字起こし API を無効化。場所の既定値は `asia-southeast1` |
| `FIRESTORE_DATABASE_ID` | 既定値 `(default)` |
| `SESSION_KEYS_JSON` | 第2段階で必須。学習署名用の鍵リング |
| `HISTORY_KEYS_JSON` | 第2段階で必須。閲覧キー用の鍵リング |
| `HISTORY_TTL_DAYS` | 既定値30。1〜3650の整数。新規保存のみに適用 |

2つの鍵リングは `{"activeKeyId":"key-id","keys":{"key-id":"<標準Base64>"}}` 形式。各鍵は32バイト以上の暗号学的乱数、鍵 ID は英数字・ハイフン・アンダースコアの1〜32文字です。用途間の鍵の使い回しは起動時に拒否します。Cloud Run では Secret Manager の固定バージョンから環境変数へ注入し、リポジトリ・ブラウザ・ログには置きません。旧鍵は対象の学習署名・保存記録が失効するまで `keys` に残します。新規発行には `activeKeyId` の鍵だけを使います。アプリが Secret Manager API を毎回呼ぶ設計ではありません。

```bash
# 必須設定を実行環境へ渡してから起動する
cd backend
go run ./cmd/server
```

鍵が両方とも空欄の場合は、第1段階の学習 API として動作します。第2段階に接続するフロントを用意した検証環境で、両方の鍵を設定してください。

Firestore の初期ルール、索引除外、`learningSessions.expiresAt` と `viewKeys.expiresAt` の TTL は [マイグレーション](../docs/MIGRATIONS.md) に従って別途設定します。API は TTL の物理削除を待たず、期限に達した記録の閲覧を拒否します。画面の保存期間案内と `HISTORY_TTL_DAYS` を一致させてください。

## 第2段階の API を通して確認する

以下は Python 標準ライブラリだけで実行できる開始→対話→保存→再送→再閲覧の例です。実行すると検証用 Gemini / Firestore を利用します。鍵・署名はプロセスメモリだけに置き、出力やシェル履歴には残しません。個人情報や実教材を使わないでください。

```bash
python3 - <<'PY'
import json
import urllib.request
base = 'http://localhost:8080'
def post(path, body):
    req = urllib.request.Request(base + path, data=json.dumps(body).encode(),
                                 headers={'Content-Type': 'application/json'})
    with urllib.request.urlopen(req, timeout=80) as res:
        return json.load(res)
content = '配列は複数の値を順番に並べます。'
start = post('/api/start', {'learningContent': content})
history = [{'role': 'model', 'content': start['reply']}]
body = {'sessionId': start['sessionId'], 'learningContent': content,
        'history': history, 'sessionProof': start['sessionProof']}
answer = '値を順番に並べて位置で取り出すものです。'
chat = post('/api/chat', {**body, 'message': answer})
history += [{'role': 'user', 'content': answer}, {'role': 'model', 'content': chat['reply']}]
body['sessionProof'] = chat['sessionProof']
saved = post('/api/summary', body)
assert saved['saved'] is True
assert post('/api/summary', body) == saved
opened = post('/api/history/open', {'viewKey': saved['viewKey']})
assert opened['summary'] == saved['summary']
print('開始・対話・保存・同一再送・再閲覧を確認しました')
PY
```

- 開始は `sessionId` / `reply` / `sessionProof` / `sessionExpiresAt`、対話は `reply` / 更新した `sessionProof` を返します。学習期限は開始成功から2時間で延長しません。
- 振り返りは `sessionId` / `summary` / `saved: true` / `viewKey` / `savedAt` / `expiresAt` を返します。`summary` は `understood` と `review` の配列です。
- 再閲覧は `sessionId` / `learningContent` / `history` / `summary` / `savedAt` / `expiresAt` を返します。内部のダイジェスト・鍵 ID・nonce・署名期限は返しません。
- エラーは `{"error":{"code":"INVALID_SESSION","message":"..."}}` 形式です。401は署名不正・期限切れ、409は別内容で終了済み、404は無効・未保存・期限切れの閲覧キー、503は保存先の障害やコミット結果不明です。生成の失敗は502、生成タイムアウトは504です。
- 503や通信切断の後は、本文・確定履歴・署名を変えずに `/api/summary` を再送します。既存記録があれば再生成せず同じ振り返り・日時・閲覧キーを返します。2時間の学習期限を過ぎると再送による保存結果の取得もできません。
- Gemini は Firestore トランザクションの外で呼びます。並行終了で生成が複数回行われても、記録とキーの索引は同時に1組だけ作り、先に確定した結果を採用します。失敗・成否不明では `saved: true` を返しません。

## 音声文字起こし

音声文字起こしは、音声ファイルをローカルに用意したうえで次のように呼ぶ。サポート形式は WebM/Opus、Ogg/Opus、MP4/AAC。ブラウザは30秒で停止を要求し、APIは実音声31秒以下、本文5 MiBまで受け付ける。返った文字は自動送信せず、内容を確認してから `/api/chat` へ送る。

```bash
curl -sS http://localhost:8080/api/speech/transcribe \
  -H 'Content-Type: audio/webm; codecs=opus' \
  --data-binary @answer.webm
```

成功時は `{"text":"..."}`、失敗時は共通のJSONエラーを返す。音声はデータベースやCloud Storageに保存しない。

質問の読み上げは、`/api/start` または `/api/chat` で返った `reply` を送る。MP3 を返し、失敗した場合はJSONエラーを返す。入力は4 KiB・300文字まで、出力音声は2 MiBまで。読み上げが失敗しても、質問の文字表示と会話履歴はそのまま使う。

```bash
curl -sS http://localhost:8080/api/speech/synthesize \
  -H 'Content-Type: application/json' \
  --data-binary '{"text":"配列とは何ですか？"}' \
  --output /tmp/ai-ni-oshieru-question.mp3
```

Cloud Text-to-Speech は `ja-JP-Chirp3-HD-Puck` と MP3 を使い、声・言語の指定はブラウザから受け取らない。ローカルの認証には上記の ADC を使う。API の有効化、権限、課金状態は対象プロジェクトで確認する。

ローカルのユーザー ADC に割り当て先プロジェクトがない場合は、プロセス単位で `GOOGLE_CLOUD_QUOTA_PROJECT="$GOOGLE_CLOUD_PROJECT" go run ./cmd/server` として指定できる。指定先では TTS API が有効で、呼び出し主体が `serviceusage.services.use` を持つ必要がある。TTS API が有効でも割り当て先が別プロジェクトなら設定エラーになることがあるため、API の有効化先と ADC の割り当て先を照合する。認証ファイルや IAM を自動で変更しない。

## Cloud Run のビルド

音声形式と MP3 の検証に `ffmpeg` / `ffprobe` を使うため、`backend/Dockerfile` を追加している。Cloud Run の GitHub 連携でソースをビルドする場合、ビルド設定を Dockerfile に切り替え、Dockerfile の場所を `backend/Dockerfile`、ビルドコンテキストを `backend` にする。Buildpacks を選んだままではこのDockerfileは使われない。Cloud Run の実行用サービスアカウントで Speech-to-Text と Text-to-Speech を呼ぶ。ブラウザやイメージに鍵を含めない。

## 確認

```bash
gofmt -l .
go vet ./...
go test ./...
go test -race ./...
```

テストは Gemini とSpeech-to-Textの代替を使用し、クラウドの認証を要求しない。実音声の検証テストはLinuxとffmpeg / ffprobeが必要。`REQUIRE_AUDIO_TESTS=1 go test -race ./...` で、未実施をスキップではなく失敗にできる。30秒・30.5秒・31秒・超過の境界、長さ情報のないWebM、時刻を圧縮した長すぎる音声、時刻の空白、途中で切れた音声、形式不一致、HTTP上限、認識応答の構造を確認する。音声の長さは復号サンプル数と再生時刻の両方で検証し、ffmpegが終了コード0でも破損の診断を出した音声は拒否する。実モデルでの質問・振り返りの品質は [評価例](../docs/API_DESIGN.md#gemini-への指示と評価例) に従って別途確認する。デプロイは [開発ルール](../docs/DEVELOPMENT_RULES.md#デプロイ手順実装後) を参照。

テストはモデルとトランザクションストアの代替を使い、署名改変・期限、再送・競合・衝突・保存結果不明、鍵再生成照合、HTTP のエラーを検証します。実 Firestore の SDK トランザクション動作、IAM、TTL、実モデルの品質はこの単体テストでは検証しません。検証環境で [確認シナリオ](../docs/DEVELOPMENT_RULES.md#確認するシナリオ) と [モデル評価例](../docs/API_DESIGN.md#gemini-への指示と評価例) を別途通してください。
