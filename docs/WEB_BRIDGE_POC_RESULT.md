# Web Bridge PoC結果

## 判定

`GO WITH CHANGES`

汎用ローカルWebサーバーとChapterBrake固有バックエンドをUnixドメインソケットで
接続し、同一オリジンの静的配信、JSON API、SSE、キャンセル伝播が成立した。
製品実装へ進める技術的な見込みはあるが、実行中エンコードを維持するため、汎用
サーバーの子プロセス終了猶予を製品契約へ追加する必要がある。

Stage 2は、利用者がこの判定、最終サーバー契約、リポジトリ名と配置先を承認するまで
開始しない。

## 実施日と環境

- 日付: 2026-07-29
- macOS 26.5.2
- Go 1.26.5
- Google Chrome 150.0.7871.187
- Microsoft Edge 150.0.4078.105
- PoC配置: `poc/stage1-web-bridge/`
- HTTP: `127.0.0.1:18765`
- バックエンド接続: Unixドメインソケット

Microsoft Edgeは確認開始時に存在しなかったため、利用者承認後にHomebrew Caskで
インストールした。

## PoC構成

```text
poc/stage1-web-bridge/
├── generic-server/
│   ├── cmd/local-web-server/
│   └── internal/server/
├── chapter-brake/
│   ├── app.json
│   ├── cmd/chapterbrake-poc-backend/
│   ├── internal/backend/
│   └── web/
└── runtime/                       # Git管理外の生成物
```

汎用サーバーはマニフェスト、アプリ一覧、静的配信、子プロセス起動、API中継、
プロセス状態だけを扱う。ChapterBrake、HandBrake、動画、キューの意味は持たない。

ChapterBrake PoCバックエンドは、既存の`queue.json`と`state.json`を読み取り専用で
公開する。実エンコード経路は呼び出さない。

汎用サーバーのCSSはアプリ一覧だけに適用し、ChapterBrakeのHTML/CSS/JavaScriptは
ChapterBrake側の`web/`が所有する。

## 実行した主な検証

```sh
GOCACHE=/private/tmp/chapterbrake-stage1-gocache make check
GOCACHE=/private/tmp/chapterbrake-stage1-gocache make install

./runtime/local-web-server \
  -listen 127.0.0.1:18765 \
  -apps "$PWD/runtime/apps" \
  -runtime "$PWD/runtime/run"
```

実通信では`curl`、Codex in-app browser、Google Chrome、Microsoft Edgeを使用した。

## 結果

| 項目 | 結果 | 確認内容 |
|---|---|---|
| `127.0.0.1`限定 | PASS | 他のlisten hostは起動時に拒否 |
| マニフェスト | PASS | schema、ID、相対パス、重複、root外参照を検証 |
| 静的配信 | PASS | `/apps/chapter-brake/`と相対CSS/JS |
| JSON API | PASS | UDS経由で実`queue.json`、`state.json`を読取 |
| SSE | PASS | `snapshot`とheartbeatを逐次flush |
| キャンセル | PASS | HTTPクライアント切断をバックエンドcontextで観測 |
| 特殊文字 | PASS | 日本語、空白、`#`、`&`を静的パスとAPI queryで往復 |
| Chrome | PASS | DOM、JavaScript、SSE状態、1440x900描画 |
| Edge | PASS | DOM、JavaScript、SSE状態、1440x900描画 |
| バックエンド異常終了 | PASS | 汎用サーバーは継続し、状態APIと中継APIでエラーを明示 |
| ブラウザ異常表示 | PASS | SSE切断後に接続失敗と警告を表示 |
| サーバー停止 | PASS | 子バックエンド終了、socket削除、HTTP停止 |
| アプリ固有スタイル | PASS | 汎用一覧とChapterBrake CSSを分離 |

ChromeとEdgeの描画結果は同じで、バックエンド準備完了、キュー0件、`idle`、
SSE接続済み、特殊文字パスの完全一致を確認した。

## 検出した制約

1. macOSのUnixソケットパスには短い上限がある。Goテストの深い`TempDir`では
   `bind: invalid argument`となった。汎用サーバーは短い専用runtime directoryを
   使用し、起動前にsocketパス長を検証する必要がある。
2. Codexの通常サンドボックスではUnixソケットとlocalhostのbindが禁止される。
   PoC自動試験と実ブラウザ確認は承認済みのサンドボックス外で実行した。
3. PoCの汎用サーバーは停止時に2秒後の強制終了を持つ。完成ChapterBrakeの
   「現在ジョブ完了後に停止」を維持するには短すぎる。
4. バックエンド異常終了後は自動再起動せず、利用者へ明示する。無限再起動を避ける
   この方針は妥当だが、製品契約として確定が必要である。

## 提案する最終サーバー契約

- 配置は`app.json`、`web/`、`bin/<backend>`。
- `/apps/<id>/`はアプリ側の静的ファイルを配信する。
- `/apps/<id>/api/...`は、先頭の`/apps/<id>`を除いた`/api/...`として中継する。
- 汎用サーバーが短いUnixソケットパスを割り当て、`LOCAL_WEB_SOCKET`で渡す。
- 通常JSONとSSEを同じReverseProxyで扱い、ストリームは即時flushする。
- HTTPリクエストのcontext cancellationをバックエンドへ伝播する。
- バックエンドの終了状態と最後のエラーを汎用状態APIで公開する。
- 異常終了したバックエンドを無限に自動再起動しない。
- マニフェストへ汎用的な`shutdown_timeout_seconds`を追加する。
  `0`は強制終了なしを表し、ChapterBrakeは`0`を指定して現在ジョブ完了まで待つ。
- 汎用サーバーと各アプリのCSSを共有せず、アプリごとに`web/`で所有する。

## 製品実装時の変更範囲

- 汎用サーバーを別リポジトリで製品化する。
- ChapterBrakeへHTTP API、SSE、実行状態コーディネーター、静的Web UIを追加する。
- 現在TUIが所有するRunnerコールバック、進捗、ETA、一時停止予約、終了予約を
  UI非依存のアプリケーション層へ移す。
- 既存のメディア、キュー、Runner、プロセス、メタデータ、JSON形式を維持する。
- 利用者の最新方針に従い、製品切替ではTUIとWebを恒久共存させない。
  Stage 1 PoCでは製品TUIを変更していない。

## 汎用サーバーのリポジトリ名候補

最終名は未確定であり、リモートリポジトリも作成していない。

1. `local-web-app-server`
2. `local-app-hub`
3. `personal-web-apps`
