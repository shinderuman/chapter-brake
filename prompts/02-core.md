# Milestone 1指示

`docs/POC_RESULT.md`が存在し、最終判定が明示的に`GO`であることを確認してください。`GO WITH CHANGES`、`NO-GO`、未作成、必須試験未完了のいずれかなら、Milestone 1を開始せず停止してください。

Milestone 1だけを実装してください。TUIと実HandBrakeCLI実行はまだ作らないでください。

- Goモジュールと最小パッケージ構成
- settings/queueモデル、検証、原子的JSON読み書き
- チャプター選択から範囲生成
- 23分40秒近似の初期選択
- 出力名と次番号計算
- PoCで確定し証拠が残っているHandBrakeCLI、mkvpropedit、ffmpeg、ffprobe引数生成の純粋ロジック
- 出力stemからのタイトル生成
- コンテナ別一時ファイル遷移
- テーブル駆動テスト

作業中は`PLANS.md`を更新し、最後に`gofmt`、`go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`を自分で実行してください。失敗を残さないでください。
