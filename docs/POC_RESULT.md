# PoC結果

## 最終判定

`GO`

チャプター境界の重複・欠落はHandBrake GUIでも再現し、従来運用で実害がなかった
ため、HandBrake固有の挙動として許容し自動補正しない要件へ変更した。P-03は合格。
完成MKV/MP4の表示タイトルもVLCで直接確認し、P-06は合格とした。

音声の本質的要件を、任意ビットレート入力ではなく、選択した各入力トラックから
プリセット由来の高品質・標準品質を作ることへ確定した。入力1のみ、2のみ、1+2、
高品質パススルー非対応時の明示フォールバック、MP4プリセットを実エンコードし、
すべて合格した。

P-01からP-10まで未確認・不合格はなく、製品実装へ進める。

## 実行環境

- macOS: 26.5.2 (25F84), arm64, Apple M1
- Go: go1.26.5 darwin/arm64
- HandBrakeCLI: 1.11.2 (2026060700)
- ffprobe/ffmpeg: 8.1.2
- mkvpropedit: MKVToolNix 100.0
- その他ツール: mkvmerge 100.0、jq、Homebrew

## 検証素材

- 生成方法: `poc/generate_fixture.sh`
- チャプター: 3秒×4（赤、緑、青、黄）
- 音声: 440Hz AC-3 5.1chと880Hz AC-3 5.1ch
- 字幕: 日本語SRT、英語SRT
- 追加素材: 30秒×4へ拡張した120秒MKV
- 実利用素材: 2.18GB、映像1・音声2・PGS字幕2・チャプター4のMKV

## 試験結果

| ID | 試験 | 結果 | 証拠・ログ | 備考 |
|---|---|---|---|---|
| P-01 | 環境・プリセット・コンテナ | 合格 | `environment-versions.txt`, `handbrake-gui-preset-list.txt`, 各ffprobe JSON | GUI取込と`FileFormat`判定を確認 |
| P-02 | 入力解析と番号対応 | 合格 | `handbrake-scan.stdout.log`, 音声astatsログ | HandBrake `TrackNumber` 1/2を実出力で識別 |
| P-03 | チャプター範囲 | 合格 | `chapter-boundary-analysis.tsv`, `long-range-2.stderr.log`, `gui-long-range-2.activity.log`, `gui-boundary-runs.tsv` | CLIとGUIで同等。境界補正なし |
| P-04 | 音声 | 合格 | `audio-quality-*.ffprobe.json`, `audio-quality-verification.tsv`, 各stderrログ | 入力1/2/1+2、明示フォールバック、MP4を確認 |
| P-05 | 字幕・非焼き付け | 合格 | `media-verification.tsv`, 字幕別ffprobe JSON | 全デコード映像ハッシュ一致 |
| P-06 | タイトルメタデータ後処理・表示 | 合格 | metadata各JSON/hash/diff、大容量比較、`vlc-*-title.jpeg` | MKV/MP4処理とVLC直接表示を確認 |
| P-07 | 既存出力 | 合格 | `existing-output-result.txt`, delete failureログ | 事前削除と削除失敗検出が成立 |
| P-08 | 即時中断 | 合格 | `interrupt-*.result.json`, restartログ | 3コマンド停止・残留なし・削除後再実行 |
| P-09 | ログ・進捗 | 合格 | `log-tee-verification.txt`, childログ | 完全tee一致、JSON進捗取得 |
| P-10 | TUIライブラリ | 合格 | `poc/tui-spike`, test/race/vet/buildログ | tview v0.42.0 |

ログの基準ディレクトリは`poc/logs/`。生成メディアは`poc/artifacts/`。

## 確定した実装方式

### メディア解析

HandBrakeCLI `--json --scan`を使い、チャプターとHandBrake音声・字幕番号を取得する。
ffprobeは出力検証とメタデータ比較に使う。ffprobe indexをHandBrake番号として扱わない。

### プリセット取得とコンテナ判定

PoCでは`--preset-import-gui`でGUIカスタムプリセットを取り込めることと、
JSONの`FileFormat`から`av_mp4` / `av_mkv`を判定できることを確認した。
製品は後続の利用者判断によりGUIへ依存せず、ChapterBrake内蔵の
`MP4 Presets`、`MKV Presets`、`My Old Presets`、`GCCX`を既定候補とする。
低解像度2件は同じ480p MP4だが、前者の自動クロップと後者のクロップなしを
別選択肢として維持する。その他の候補はHandBrakeCLIの標準プリセット一覧から
取得する。

### チャプター範囲指定

`--chapters N-M`を使用する。GUIでも発生する素材依存の境界重複・欠落は
HandBrake固有の挙動として許容し、秒・フレーム・PTS指定や後処理による
自動補正は行わない。

### 音声引数

```text
入力1:
--audio 1,1
--aencoder copy:ac3,ca_aac
--ab 640,160
--mixdown 5point1,stereo
--arate auto

入力2:
--audio 2,2
--aencoder copy:ac3,ca_aac
--ab 640,160
--mixdown 5point1,stereo
--arate auto

入力1+2:
--audio 1,1,2,2
--aencoder copy:ac3,ca_aac,copy:ac3,ca_aac
--ab 640,160,640,160
--mixdown 5point1,stereo,5point1,stereo
--arate auto
```

数値は利用者入力にしない。PoCではGUIプリセットの値で経路を証明したが、
製品は映像プリセットから独立したChapterBrake音声方針として、高品質は
可能ならパススルー、非対応時はAAC 640 kbps・5.1ch、標準品質は
AAC 160 kbps・stereoとする。

入力が高品質パススルーに非対応の場合、`copy:ac3`をそのまま渡すとHandBrakeCLIが
その音声を削除した。製品ではスキャン結果で互換性を事前判定し、非対応時は
ChapterBrakeのAACフォールバックを明示する。PoCのAAC入力では次が成立した。

```text
--audio 1,1
--aencoder ca_aac,ca_aac
--ab 640,160
--mixdown 5point1,stereo
--arate auto
```

出力は高品質AAC 5.1chと標準品質AAC stereoの2本になり、欠落しなかった。

### 字幕引数

字幕なし:

```text
--subtitle none --subtitle-burned=none --subtitle-default=none
```

MKVソフト字幕:

```text
--subtitle 1[,2] --subtitle-burned=none --subtitle-default=none
```

### MKVタイトル設定

```text
mkvpropedit <encode-temp> --edit info --set title=<stem>
```

### MP4タイトル設定

```text
ffmpeg -i <encode-temp> \
  -map 0:v -map 0:a? -map_metadata 0 -map_chapters 0 -c copy \
  -metadata title=<stem> -brand <入力major_brand> \
  <metadata-temp>
```

`-map 0`はチャプター用dataを重複させるため使用しない。

### 最終ファイル公開

titleと構造をffprobeで検証後、同一ディレクトリの一時ファイルを最終名へrenameする。

### プロセス中断

新規プロセスグループで起動し、グループへSIGINT、期限後SIGKILL、`Wait`、
部分出力削除の順とする。HandBrake/ffmpegはシグナルを処理した終了コード、
mkvpropeditはsignal終了として区別できる。

### ログと進捗

stdout/stderrをストリームで別ログへ書きながらTUIへteeする。
HandBrake `--json`のProgressを表示に使い、パース失敗でも生ログ保存を継続する。

### TUIライブラリ

tview v0.42.0で必要ウィジェットとキャンセル捕捉が成立したため、初期版で採用する。

## 設計変更

- 変更内容: GUIと同等のチャプター境界挙動を許容し自動補正しない。タイトル表示は
  VLC直接確認を必須証拠とする。音声の任意ビットレート入力を廃止し、
  各選択入力からプリセット由来の高品質・標準品質を生成する。
- 理由: 従来GUI運用で境界挙動の実害がなく、Twonky経由確認は利用者判断で不要となった。
  音声の目的は数値指定ではなく、高品質版と標準品質版を持つことである。
- 再試験した項目: P-03をHandBrake GUI、P-06をVLC、P-04を入力1/2/1+2、
  パススルー非対応入力、MP4プリセットで再試験した。

## 未解決事項

必須項目に未解決事項はない。完成アプリの実利用で新たな問題が確認された場合は、
その時点で別課題として再検討する。

## GO判定の根拠

P-01からP-10がすべて合格し、チャプター、音声、字幕、タイトル後処理、
上書き、中断、ログ、TUIの具体的な実装経路が確定したためGOとする。
