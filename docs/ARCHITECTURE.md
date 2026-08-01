# Goアーキテクチャ方針

## 1. 原則

- 小規模なローカルアプリとして理解しやすい構成を優先する。
- ドメインロジック、永続化、外部コマンド、Web境界を分離する。
- レイヤー数を増やすこと自体を目的にしない。
- 実HandBrakeCLI、ffmpeg、mkvpropeditがなくても、範囲生成、命名、JSON、各コマンド引数生成、タイトル値決定、キュー進行を単体テストできるようにする。

## 2. 想定パッケージ

```text
cmd/chapterbrake
internal/bootstrap
internal/app
internal/config
internal/instance
internal/queue
internal/media
internal/handbrake
internal/metadata
internal/runner
internal/runstate
internal/logging
internal/control
internal/webapi
web
```

### `cmd/chapterbrake`

- `main`のみ。
- `internal/bootstrap.Run`の呼び出しと終了コードだけを扱う。
- 製品ロジックを置かない。

### `internal/bootstrap`

- OSシグナルをアプリの停止経路へ接続する。
- 設定、キュー、ログ、外部ツール、ランナー、Web APIの依存関係を組み立てる。
- ドメイン判断を持たず、各パッケージの実装を起動可能な形へ配線する。

### `internal/app`

- 画面フローとユースケースの調整。
- ファイル解析、ジョブ生成、キュー追加、キュー実行を結ぶ。
- Web固有メッセージやHandBrakeCLIの細部を持ち込まない。

### `internal/config`

- データディレクトリとmacOS標準ログディレクトリの解決。
- 入出力ディレクトリとチャプター区切り時間を保持する`settings.json`の読み書き、
  既定作成、検証、既知の旧形式からの移行。

### `internal/queue`

- ジョブモデル。
- `queue.json`の読み書き、検証、原子的保存。
- append、delete、move、claim-head、complete-headなど最小操作。
- 配列順を実行順とする。
- 同一プロセス内の追加、削除、実行開始、先頭完了をmutexで直列化する。
- 実行開始した先頭ジョブを占有し、完了または中断まで削除を拒否する。

### `internal/media`

- 入力MKVの解析モデル。
- チャプター、音声、字幕の型。
- ffprobe等の具体実行は小さな境界として分離する。

### `internal/handbrake`

- プリセット一覧とコンテナ判定。
- GUIエクスポートJSONの既定一覧化と`--preset-import-file`引数生成。
- ジョブからHandBrakeCLI引数配列を生成。
- バージョン・能力確認。
- シェル文字列を生成しない。

### `internal/metadata`

- 最終出力パスからタイトル文字列を決定。
- MKV用mkvpropedit引数を生成。
- MP4用ffmpegストリームコピー引数を生成。
- ffprobe等によるタイトル・ストリーム・チャプター検証。
- コンテナ別の一時ファイル遷移を表現する。
- シェル文字列を生成しない。

### `internal/runner`

- キュー先頭の逐次実行。
- ジョブごとに先頭を占有し、成功後に原子的に完了してからキューを再読込する。
- 実行中に追加された後続ジョブを同じ実行セッションで処理する。
- 現在ジョブ完了後の一時停止。
- HandBrakeエンコード中のプロセス一時停止・再開。
- 即時キャンセル。
- HandBrakeエンコード、コンテナ別タイトル設定、検証、最終renameのオーケストレーション。
- 一時出力と不完全出力の削除。
- 成功時のみ先頭削除。
- 外部プロセス終了待ちとエラー分類。

### `internal/runstate`

- `state.json`の厳格な読み書きと原子的保存。
- 実行中、失敗、前回異常終了の最小情報を保持する。
- 進捗率や再開用エンコーダ状態は保持しない。

### `internal/logging`

- アプリログ。
- ジョブログ作成。
- stdout/stderrのtee。

### `internal/control`

- Webバックエンドのキュー実行状態を所有する。
- `runner.Runner`の進捗、段階、ジョブログ、一時停止、完了コールバックを
  JSONで表現可能なスナップショットへ変換する。
- HandBrake一時停止、再開、現在ジョブ後停止、即時中断、通常終了待ちを
  既存Runnerへ委譲し、キューや外部CLI引数を独自実装しない。

### `internal/webapi`

- `LOCAL_WEB_SOCKET`で指定されたUnixドメインソケットへHTTP APIを公開する。
- 入力選択、設定更新、キュー操作を型付きJSONで既存`app.Service`へ接続する。
- 状態、進捗、ETA、ジョブログ差分をSSEで配信する。
- 任意コマンド、任意出力パス、HandBrakeCLI引数を受け取らない。
- API契約は`docs/WEB_API.md`を正本とする。

### `web`

- ChapterBrake固有のHTML、CSS、ES Modulesを所有する。
- `docs/WEB_API.md`の型付き操作だけを呼び、外部CLI引数を構築しない。
- SSEスナップショットとログ差分を画面状態へ反映する。
- ブラウザ再読み込みや切断をキュー実行の停止へ結び付けない。
- 汎用Local Web App Serverの画面やCSSを共有しない。
- SortableJSはChapterBrakeの`web/vendor/`へバージョン固定して同梱する。汎用サーバーは
  各アプリ固有のUIライブラリと更新周期を所有しない。

### `internal/instance`

- ユーザーデータディレクトリのadvisory lockをアプリ存続中保持する。
- 二重起動による`queue.json`の競合更新と複数同時エンコードを拒否する。

## 3. モデル例

実際の名前はGoの慣例に合わせて調整可能だが、概念は次を持つ。

```text
MediaInfo
  Chapters []Chapter
  AudioTracks []AudioTrack
  SubtitleTracks []SubtitleTrack

EncodeJob
  ID
  InputPath
  OutputPath
  Preset
  Container
  ChapterRange
  AudioSelections
  SubtitleTracks
```

時刻は表示用計算に使うが、EncodeJobの切り出し境界はチャプター番号だけを保持する。

## 4. 外部コマンド境界

実行境界は、テストで偽物へ置き換えられる最小インターフェースにする。

禁止:

- パッケージ全体を覆う巨大な`Service`インターフェース。
- `exec.Command`を各所から直接呼ぶ。
- HandBrakeCLI、ffmpeg、mkvpropeditの処理を一つの巨大なコマンド文字列へ埋め込む。
- `/bin/sh -c`で引数を連結する。

要件:

- 実行ファイルと引数を分ける。
- `context.Context`でキャンセル可能。
- stdout/stderrを別々または明確な順序で取得・ログ保存。
- 終了コード、シグナル、中断を区別できる。
- macOS上でHandBrakeCLI、ffmpeg、mkvpropeditが子プロセスを作る場合はプロセスグループ単位で停止する。
- HandBrakeエンコードの一時停止・再開は専用executorのプロセスグループへ
  `SIGSTOP` / `SIGCONT`を送る。

## 5. 停止設計

- 通常終了とOS終了シグナルは、現在ジョブ完了後に残りのキューを停止する。
- Web APIの即時中断だけが実行中コマンドのキャンセル経路を使う。
- 現在実行中のHandBrakeCLI、ffmpeg、mkvpropeditのプロセス参照を安全に管理する。
- 即時中断時は、まず調査で決定した穏当なシグナルを送り、期限後に強制終了する。
- HandBrakeが一時停止中なら、SIGCONTで再開してから中断シグナルを送る。
- プロセス終了確認前に出力ファイルを削除しない。
- キャンセルと通常失敗をログ・UIで区別する。
- キャンセル後もキュー先頭は残す。

## 6. JSONと不変条件

読み込み直後にモデルを検証し、不正な状態をアプリ内部へ流さない。

主な不変条件:

- 設定versionが対応範囲。
- 出力ディレクトリが絶対パス。
- ジョブIDが空でない。
- 入出力が絶対パス。
- 入力と出力が同一パスではない。
- コンテナと拡張子が一致。
- タイトルメタデータは出力パスのstemから決定できる。
- 一時ファイルは最終出力と同一ディレクトリで、最終出力とは異なる。
- チャプター範囲が正しい。
- 新形式では音声トラックごとに高音質・低音質を重複なく一つだけ指定する。
- 音声選択は空配列を許可し、無音声出力を表す。
- MP4の字幕配列は空。

## 7. テスト方針

### 純粋ロジック

- チャプター選択から範囲生成。
- 先頭未選択。
- 一件だけ選択。
- 設定可能な区切り時間によるチャプター開始位置近似。
- `分:秒`形式の区切り時間解析・整形。
- 等距離時の決定規則。
- 最終チャプターを自動開始点にしない末尾結合。
- チャプター単体時間と生成区間合計時間。
- 連番と桁数。
- 既存キュー・既存ファイルから次番号算出。
- HandBrakeCLI、mkvpropedit、ffmpeg、ffprobeの引数配列。
- 出力パスからのタイトル生成。
- MKV/MP4の一時ファイル遷移。

### 永続化

- 設定初期作成。
- 正常読み書き。
- 不正JSON。
- 未知version。
- 不正ジョブ。
- 原子的置換失敗時に正本が残ること。

### ランナー

偽コマンドを使い次を確認する。

- 成功時だけ先頭削除。
- 失敗時は先頭保持。
- 中断時は先頭保持。
- 一時停止予約時は現在ジョブ成功後に後続を実行しない。
- 実行前に既存出力削除。
- 失敗・中断時に部分出力削除。
- 後続ジョブを実行しない。

### 統合

- 小さなテストMKVを用いたffprobe/HandBrakeCLI/mkvpropedit/ffmpeg統合。
- 実ファイルを使うテストは通常の`go test ./...`から分離し、明示フラグまたは別スクリプトにする。

## 8. 依存関係

`mkvpropedit`はMKVToolNix、ffmpeg/ffprobeはFFmpegの外部実行ファイルとして
明示的なランタイム依存にする。依存は次を満たすものに限定する。

- 現在保守されている。
- macOSで安定。
- 導入により独自実装より保守コストが下がる。

依存の追加理由を`docs/DECISIONS.md`へ記録する。
