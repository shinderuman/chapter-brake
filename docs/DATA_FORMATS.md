# データ形式

## 保存場所

```text
~/Documents/ChapterBrake/
├── settings.json
├── queue.json
├── state.json
└── My Presets.json

~/Library/Logs/ChapterBrake/
├── app-*.log
└── job-*.log
```

アプリはデータディレクトリ、ログディレクトリと管理JSONを必要に応じて作成する。
`My Presets.json`は利用者がHandBrake GUIからエクスポートして配置する任意ファイルで、
アプリは自動生成しない。

## settings.json

初期形式:

```json
{
  "version": 4,
  "input_directory": "/Volumes/2TB HDD/Images",
  "output_directory": "/Volumes/2TB HDD/Movies",
  "chapter_interval": "23:40"
}
```

要件:

- `version`は必須で4。
- `input_directory`と`output_directory`は絶対パス。
- `chapter_interval`は正の`分:秒`形式とする。分は59を超えてよく、秒は
  `00`から`59`の2桁とする。
- 読み込み時に入力先の存在とディレクトリ種別を検証する。
- 出力先はジョブ開始時に必要なら作成し、その時点でディレクトリ種別と
  書き込み可能性を検証する。
- ファイルがなければ既定値で作成する。
- version 1の既知形式は`output_directory`を維持し、既定の
  `input_directory`と`chapter_interval`を追加してversion 4へ原子的に移行する。
- version 2の既知形式は入出力先を維持し、既定の`chapter_interval`を追加して
  version 4へ原子的に移行する。
- version 3は既存値を維持してversion 4へ移行する。ただし出力先が旧既定値
  `/Volumes/2TB HDD/mp4/`の場合だけ新既定値`/Volumes/2TB HDD/Movies`へ変更する。
- JSON不正または未知のversionの場合はエラーで停止し、勝手に上書きしない。
- `--directory PATH`または`-d PATH`は、一回の起動だけファイル選択の初期位置を
  指定ディレクトリへ差し替え、設定ファイルを書き換えない。相対パスも許可する。
- チャプター分割画面で変更した区切り時間もそのジョブ追加フローだけに適用し、
  `settings.json`へ書き戻さない。キューには確定したチャプター範囲だけを保存する。
- 末尾チャプター除外の選択も補助設定としてキューへ保存せず、確定した
  `chapter_start`と`chapter_end`だけを保存する。
- Web UIの設定モーダルは3項目を型付きAPIで更新し、`settings.json`へ原子的に保存する。

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
      "output": "/Volumes/2TB HDD/Movies/source/source_01.mkv",
      "preset": "My H.265 MKV",
      "preset_file": "/Users/example/Documents/ChapterBrake/My Presets.json",
      "container": "mkv",
      "chapter_start": 1,
      "chapter_end": 5,
      "duration_seconds": 1421,
      "audio_selections": [
        {"track": 1, "quality": "high"},
        {"track": 2, "quality": "standard"},
        {"track": 3, "quality": "standard"}
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
- Web UIの削除操作もジョブIDで対象を特定して原子的に保存する。実行中ジョブは
  削除を拒否する。
- Web UIの並び替え操作は隣接ジョブを入れ替えて原子的に保存する。実行中先頭は固定する。
- `input`と`output`は絶対パス。
- `container`は`mp4`または`mkv`。
- `chapter_start`と`chapter_end`は1以上で、開始<=終了。
- `audio_selections`は入力音声トラックごとの出力品質を表す。`track`は1以上で
  重複不可、`quality`は`high`または`standard`とする。空配列は無音声出力を表す。
- 高音質・低音質の具体設定はChapterBrakeの
  バージョン付き音声方針から決定し、映像`preset`の音声ルールには依存しない。
- `subtitles`は入力字幕トラック番号。MP4では空配列でなければならない。
- `preset_file`はGUIエクスポートプリセットを使うジョブの絶対パス。互換内蔵または
  HandBrake標準プリセットでは省略する。
- `duration_seconds`は選択チャプター範囲から求めた参考時間。0または省略は旧ジョブの
  時間不明を表し、エンコード境界には使わない。
- 字幕焼き付けを表すフィールドは作らない。焼き付けは常に無効という不変条件にする。
- タイトルメタデータ用フィールドは作らない。値は常に`output`のファイル名から拡張子を除いて導出する。

`id`形式は実装時に一意性とテスト容易性を満たす単純な形式へ変更可能。UUID外部依存を増やす必要はない。

## 手編集に関する扱い

- 手編集はアプリ停止中に限る。
- 正式なキュー作成方法はWeb UI。
- 手編集で不正データが入った場合、アプリは読み込みエラーで停止する。
- 欠落値の推測、範囲の自動補正、未知フィールドの意味解釈はしない。
- 不正JSONを検出した際は元ファイルを保存したまま、エラー箇所を可能な範囲で示す。

## 原子的保存

`settings.json`、`queue.json`、`state.json`は次の考え方で保存する。

1. 同一ディレクトリ内に一時ファイルを作成。
2. インデント付きJSONを完全に書き込む。
3. flush/closeエラーを確認する。
4. 必要な耐久性を検討して同期する。
5. renameで置換する。
6. 途中失敗時は既存の正本を壊さない。

ファイル権限は通常のユーザーデータとして過度に特殊化しない。

## 一時出力

一時出力は最終出力と同じ
`<output_directory>/<出力ベース名>/`ディレクトリへ置き、最終公開を
同一ファイルシステム上のrenameで行えるようにする。

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

## state.json

実行中ジョブと直近の異常停止を永続化する。`status`は`idle`、`running`、
`failed`のいずれかとし、`running`のまま次回起動した場合は前回異常終了として
`failed`へ更新する。進捗率は保存せず、`queue.json`の高頻度更新も行わない。

## My Presets.json

HandBrake GUIのエクスポートJSONをそのまま置く。GUI内部設定は読まず、
この明示ファイルだけを`--preset-import-file`でHandBrakeCLIへ渡す。

## ログ

推奨例:

```text
~/Library/Logs/ChapterBrake/
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
