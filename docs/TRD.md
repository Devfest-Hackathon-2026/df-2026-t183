# 技術要件（TRD）

## 技術スタックと選定理由

| 領域 | 採用 | 比較した案と採用理由 |
| --- | --- | --- |
| Web | React・TypeScript・Vite | HTML と JavaScript だけでも作れるが、教材入力・対話・振り返り・再閲覧の状態と API の型を揃えるため React と TypeScript にした。Next.js は SSR や Web 側のサーバー処理が要らないため見送った |
| 画面の配信 | Firebase Hosting | Cloud Run で静的ファイルも配る案、Firebase App Hosting の案と比べた。ビルドした静的ファイルを CDN から配るだけで足り、API と別に更新できる |
| 学習 API | Go、Cloud Run | Cloud Run functions や Node.js も候補だった。チームの Go の試作を継げること、コンテナをそのまま動かせて使わない時は0台まで縮むこと、ハッカソン指定の実行基盤であることから選んだ |
| 生成 AI | Vertex AI 経由の Gemini Flash 系、Google Gen AI Go SDK | Google AI Studio の API キーで呼ぶ案と比べた。チームの Cloud Credit は AI Studio に使えない。Vertex AI なら実行用サービスアカウントで認証でき、API キーを配らずに済む。Pro 系より速く安い Flash 系で足りる。コードで `genai.BackendVertexAI` を明示する |
| AI の質問の音声化（任意） | Cloud Text-to-Speech の Chirp 3 HD、Go API 経由 | ブラウザの音声合成と比べ、端末ごとの差を抑えて声を固定できる。音声化の失敗で文字の対話を止めない。課金対象の公開APIなので、入力と同時処理数を制限する |
| 保存（第2段階） | Firestore | ブラウザ内に保存する案（別の端末で開けない）、Cloud SQL（常時起動の費用と接続管理が要る）と比べた。1セッションを1ドキュメントで持て、トランザクションと TTL が標準で使え、サーバーの管理が要らない |
| PDF・TXT | ブラウザの PDF.js で本文にする | Go で文字を抜き出す案（日本語の PDF に強いライブラリが少ない）、PDF を毎回 Gemini に送る案（継続・振り返りのたびに PDF を送り直すため、量と費用が増える）と比べた。本人が本文を確かめて直せ、以後は同じ本文を使い回せる |
| 学習中の署名（第2段階） | HMAC の署名をブラウザに持たせる | 会話をサーバーの DB に毎回保存する案と比べた。対話中に DB を使わずに済み、偽の AI 発言や改ざんした履歴を保存させない |
| 鍵 | Secret Manager | 環境変数に直接書く案と比べた。リポジトリや設定ファイルに秘密を置かず、版を固定して入れ替えられる |

モデル ID とロケーションは対象プロジェクトで確認する。SDK の既定動作に依存しない。 [Google Gen AI Go SDK の設定](https://github.com/googleapis/go-genai/blob/main/client.go)

## 公開 API の守り

- IAM 付き検証環境と、ログイン不要の審査環境を分ける。審査用ブラウザは Cloud Run の公開 HTTPS URL へ直接接続する。CORS は許可オリジンのブラウザ向けで、認証や利用制限の代わりにはならない。 [Cloud Run の認証と CORS](https://docs.cloud.google.com/run/docs/authenticating/end-users)
- アプリ独自の利用回数の上限（利用枠）は作らない（2026-11-07 決定）。代わりに次で費用と負荷を抑える。
  - 入力の上限（[API 設計](API_DESIGN.md#入力上限)）と、Gemini の最大出力トークン数 `GEMINI_MAX_OUTPUT_TOKENS`。
  - Cloud Run のサービス全体の最大インスタンス数と、1インスタンスの同時処理数。上限を超えた要求は Cloud Run が429で断る。 [Cloud Run の最大インスタンス数](https://docs.cloud.google.com/run/docs/configuring/max-instances)
  - Cloud Billing の予算アラート。予算アラートは課金を止めない。 [Cloud Billing の予算](https://docs.cloud.google.com/billing/docs/how-to/budgets)
- 受け入れる危険: URL を知っている人は、最大インスタンス数の範囲で何度でも Gemini や質問読み上げを呼べる。審査期間中は予算アラートを確認し、費用が想定を超えたら、審査環境を IAM 付きに戻すか最大インスタンス数を下げる。閲覧キーは約60ビットあり、当てずっぽうで当たる心配はない（[閲覧キー](API_DESIGN.md#閲覧キー第2段階)）。

## 応答とデータ保護

- Gemini の待ち時間は最大60秒、API 全体は70秒、Cloud Run は75秒、ブラウザは80秒を設定案とする。Firebase Hosting の rewrite に変更する場合は、Hosting の60秒制限を踏まえて期限を短くする。 [Firebase Hosting と Cloud Run](https://firebase.google.com/docs/hosting/cloud-run)
- 入力前に教材・会話を送る用途、終了時の保存と期限、閲覧キーでのアクセス・削除を伝える。アプリの保存期間と Vertex AI 側のデータ取り扱いは区別する。
- 教材・履歴を権限のある指示に昇格させない。第2段階からは署名で会話の改変を検知する。振り返りは学習者の発言からの引用を検証する。署名とスキーマだけで生成の内容すべてが正しいとは判定できないため、[評価例](API_DESIGN.md#gemini-への指示と評価例) で確かめる。
- 記録・キーを URL、アプリログ、分析イベントに残さない。未完了の学習（教材・確定会話・署名・回答下書き・自己確認・凍結状態）と開始前の教材下書きだけは、最長2時間の固定期限付き localStorage で保持する。保存成功・明示破棄・期限切れで取り除き、保存済み結果・閲覧キー・音声は永続化しない。主要フォームはラベルとフォーカスを持ち、状態を色だけで伝えない。

## 実装とテストの順序

段階と日程は [PRD](PRD.md#mvp-の範囲と段階) と [開発ルール](DEVELOPMENT_RULES.md#段階分担日程) に従う。

1. 第1段階: 直接入力で開始・対話・振り返りまで、公開 URL で通す。Go のテストでは Gemini の代替を使う。評価例を実モデルで確かめる。続けて TXT と PDF の読み取りをつなぐ。
2. 第2段階: 学習中の署名、終了時の保存、閲覧キー、別ブラウザでの再閲覧。署名不正、Firestore の障害、保存応答の喪失と再送を代替で再現する。
3. 第3段階: 削除。削除と終了の再送が重なっても記録が復活しないことを確かめる。
4. [開発ルール](DEVELOPMENT_RULES.md#確認するシナリオ) のケースを通してから審査用 URL を案内する。

## 公開前に確認すること

- 対象プロジェクトの請求先、Vertex AI のモデル、Firestore と鍵の権限。プロジェクト `team183-ai-ni-oshieru` は請求先がまだつながっていない（2026-11-07 時点、増枠を申請中）。11/8 の夜までにつながらなければ、課金が有効な別のプロジェクトで公開し、つながった後に移す。移すときは Firestore の記録と鍵を作り直し、古い環境で発行した閲覧キーは引き継がない。
- PDF は日本語の通常文書、段組、文字を抽出できないスキャンで操作を確認する。
- 保存期限の運用値（初期値30日）、案内の表示、期限到達時の拒否と TTL。最大インスタンス数、同時処理数、最大出力トークン数、予算アラート。
- ログインしていないブラウザで接続できること。設定・検証結果を記録する。
