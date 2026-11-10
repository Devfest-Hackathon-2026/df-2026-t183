# Teach Back（日本語名：AIに教える）

## プロジェクト

学んだ内容を自分の言葉で AI に教え、質問と振り返りから次に復習する点を見つける Web アプリです。学習はログイン不要で、終了時に発行する閲覧キーで記録を開き直せる設計です。DevFest GDGoC Japan Hackathon 2026 の教育カテゴリで制作しています。

- [仕様書の目次](docs/README.md)
- [アプリ概要](docs/app-overview.md)
- [API 設計](docs/API_DESIGN.md)

## 公開サイトとリポジトリ

- 公開サイト: https://team183-ai-ni-oshieru.web.app/
- 開発用（非公開）: `yasusudas/ai-ni-oshieru`
- 公式提出用（公開）: [Devfest-Hackathon-2026/df-2026-t183](https://github.com/Devfest-Hackathon-2026/df-2026-t183)

両リポジトリを併存させ、検証したアプリと仕様書をそれぞれの PR を経由して `main` に反映します。公式側には確認済みのファイルだけを追加し、非公開リポジトリの履歴やローカルの設定・資格情報は移しません。GitHub の更新と公開サイトへの配信は別操作です。

公式リポジトリでは GitHub Actions と GitHub Pages を利用できません。運営が用意した [提出リポジトリの案内](docs/SUBMISSION_GUIDE.md) も確認してください。

## 開発状況

直接入力・UTF-8 TXT・PDF の教材を使い、ゼロとの対話から振り返りまで進めます。画面は `web/`、学習 API は `backend/` にあります。第2段階のバックエンドには、学習中の署名、振り返り完了時の Firestore 保存、閲覧キーによる再閲覧を追加しています。フロントの接続は [第2段階の連携手順](docs/PHASE2_FRONTEND_HANDOFF.md) を参照してください。

両方の起動手順は [開発ルール](docs/DEVELOPMENT_RULES.md#ローカルでの確認実装後) を参照してください。
