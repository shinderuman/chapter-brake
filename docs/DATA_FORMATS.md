# データ形式

## 保存場所

```text
~/Documents/ChapterBrake/
├── settings.json
├── queue.json
└── logs/
```

アプリはディレクトリがなければ作成する。

## settings.json

初期形式:

```json
{
  "version": 1,
  "output_directory": "/Volumes/2TB HDD/mp4/"
}
```

要件:

- `version`は必須で1。
- `output_directory`は絶対パス。
- 読み込み時に存在、ディレクトリ種別、書き込み可能性を検証する。
- ファイルがなければ既定値で作成する。
- JSON不正または未知のversionの場合はエラーで停止し、勝手に上書きしない。
- 初期版では設定編集UIを作らない。

## queue.json

推奨初期形式:

```json
{
  "version": 1,
  "jobs": [
    {
      "id": "20260723T143012.123456789-0001",
      "created_at": "2026-07-23T14:30:12.123456789+09:00",
      "input": "/Volumes/Video/source.mkv",
      "output": "/Volumes/2TB HDD/mp4/source_01.mkv",
      "preset": "My H.265 MKV",
      "container": "mkv",
      "chapter_start": 1,
      "chapter_end": 5,
      "audio": [
        {
          "source_track": 1,
          "bitrate_kbps": 640
        },
        {
          "source_track": 2,
          "bitrate_kbps": 160
        }
      ],
      "subtitles": [1]
    }
  ]
}
```

### 意味

- `jobs`の配列順が実行順。
- 先頭ジョブが実行中でも成功するまで残る。
- `status`は初期版では持たない。
- 成功時に先頭要素だけ削除する。
- `input`と`output`は絶対パス。
- `container`は`mp4`または`mkv`。
- `chapter_start`と`chapter_end`は1以上で、開始<=終了。
- `audio`は選択された入力音声トラックとビットレート。初期版はトラック1・2だけを許可する。
- `subtitles`は入力字幕トラック番号。MP4では空配列でなければならない。
- 字幕焼き付けを表すフィールドは作らない。焼き付けは常に無効という不変条件にする。
- タイトルメタデータ用フィールドは作らない。値は常に`output`のファイル名から拡張子を除いて導出する。

`id`形式は実装時に一意性とテスト容易性を満たす単純な形式へ変更可能。UUID外部依存を増やす必要はない。

## 手編集に関する扱い

- 手編集はアプリ停止中に限る。
- 正式なキュー作成方法はTUI。
- 手編集で不正データが入った場合、アプリは読み込みエラーで停止する。
- 欠落値の推測、範囲の自動補正、未知フィールドの意味解釈はしない。
- 不正JSONを検出した際は元ファイルを保存したまま、エラー箇所を可能な範囲で示す。

## 原子的保存

`settings.json`と`queue.json`は次の考え方で保存する。

1. 同一ディレクトリ内に一時ファイルを作成。
2. インデント付きJSONを完全に書き込む。
3. flush/closeエラーを確認する。
4. 必要な耐久性を検討して同期する。
5. renameで置換する。
6. 途中失敗時は既存の正本を壊さない。

ファイル権限は通常のユーザーデータとして過度に特殊化しない。

## 一時出力

一時出力は最終出力と同じディレクトリへ置き、最終公開を同一ファイルシステム上のrenameで行えるようにする。

概念例:

```text
.chapterbrake-<job-id>-encode.mkv
.chapterbrake-<job-id>-encode.mp4
.chapterbrake-<job-id>-metadata.mp4
```

- 実際の命名は衝突しないこと、ログから対応を追えることを満たせば変更可能。
- MKVはエンコード用一時ファイルをmkvpropeditで直接編集し、検証後に最終名へrenameする。
- MP4はエンコード用一時ファイルからメタデータ用一時ファイルを作り、検証後に後者を最終名へrenameする。
- 成功・失敗・中断のいずれでも不要な一時ファイルを残さない。

## ログ

推奨例:

```text
logs/
├── app-2026-07-23.log
├── job-20260723T143012-source_01.log
└── job-20260723T151455-source_02.log
```

### アプリログ

`slog`のTextHandlerなど、人間が読みやすく検索もしやすい形式を使う。

記録対象:

- 起動、終了
- 設定とキュー読み込み結果
- ジョブ追加
- キュー開始・完了
- ジョブ開始・成功・失敗・中断
- 出力削除
- 外部コマンドのパスとバージョン（HandBrakeCLI、ffprobe、ffmpeg、mkvpropedit）
- エラーと関連ログパス

### ジョブログ

ファイル先頭に次を記録する。

- ジョブID
- 入力、出力
- プリセット
- チャプター範囲
- 音声、字幕
- 実行開始時刻
- 実際に`os/exec`へ渡したHandBrakeCLI、mkvpropedit、ffmpeg、ffprobeの実行ファイルと引数配列
- エンコード用一時パス、メタデータ用一時パス、最終出力パス
- 設定したタイトルと検証結果

続けてHandBrakeCLI、mkvpropedit、ffmpeg、ffprobeの標準出力・標準エラーを欠落なく保存する。終了コード、中断シグナル、終了時刻も記録する。

初期版ではログの自動削除、圧縮、ローテーションを行わない。
