# Stage 1 Web Bridge PoC

製品コードから隔離した、汎用ローカルWebサーバーとChapterBrake最小バックエンドの
接続確認用PoC。

- `generic-server/`: アプリ固有知識を持たない静的配信、子プロセス起動、UDS API中継
- `chapter-brake/`: `queue.json`と`state.json`を読み取り専用で公開する最小バックエンド
- `runtime/`: ビルド後のマニフェスト、Web、バイナリ配置

実エンコード処理は呼び出さない。
