# 完成アプリ受け入れ結果

実施日: 2026-07-26
最終判定: **PASS**

## 使用環境

- macOS
- Go 1.26.5
- HandBrakeCLI 1.11.2
- FFmpeg / ffprobe 8.1.2
- mkvpropedit 100.0
- VLCによる完成MKV/MP4の直接表示確認

実ツールのパスとバージョンは、完成アプリの初回起動時に
当時の`~/Documents/ChapterBrake/logs/app-2026-07-26.log`へ記録されることも確認した。
2026-07-28以降の新規ログは`~/Library/Logs/ChapterBrake/`へ保存する。

## 追加保守変更の検証

実利用タイトル`くまクマ熊ベアーぱーんち！ 第3巻_t00.mkv`を
HandBrakeCLI 1.11.2で再スキャンし、全長1:34:45、Chapter 21が16:31であることを
確認した。23:40近似の回帰テストでは開始位置を`1,7,13,19`とし、最終出力を
Chapter 19-21の約23:41へ結合する。

キュー一覧と実行状況の統合、ジョブ詳細からの削除・一時停止・即時中断、
`j`/`k`並び替え、キュー追加後の自動開始、通常終了時のジョブ境界停止、
タイトル名ディレクトリへの出力を単体・TUIシミュレーション・race試験で確認した。
更新版は`make`で`~/.local/bin/chapterbrake`へインストールし、ビルド成果物との
SHA-256一致と`--help`起動を確認した。

### 2026-07-28 キュー・一時停止・プリセット・障害表示

- GUIエクスポート`My Presets.json`を製品パーサーで読み、4件の名前、MP4/MKV、
  解像度、クロップ差を確認した。`HandBrakeCLI --preset-import-file ... --preset-list`
  の実行でも`MP4 Presets`、`MKV Presets`、`My Old Presets`、`GCCX`が
  `My Presets/`配下に列挙された。
- `~/Downloads/My Presets.json`を`~/Documents/ChapterBrake/My Presets.json`へ
  コピーし、両者のSHA-256一致を確認した。コピー元は保持した。
- 実HandBrakeCLI 1.11.2を製品の`OSExecutor`で起動し、SIGSTOP後のプロセス状態が
  `T`になること、SIGCONT後に通常のキャンセル経路で終了できることを
  `TestRealHandBrakePauseResume`で確認した。
- TUIシミュレーションで、メインとキュー一覧・詳細の参考動画時間、全体ETA、
  失敗時の赤枠、チャプター表の固定列、ファイル画面の左キー無効化、
  `k`連打時に同じ待機ジョブとカーソルが連続移動することを確認した。
- ジョブ追加後はキュー実行を開始しながら同じ入力ディレクトリへ戻り、
  タイトル名の出力子ディレクトリはキュー追加時でなく実行開始時に作ることを
  単体・TUI試験で確認した。
- 既存ログ417ファイル・96,985,854 bytesを`~/Library/Logs/ChapterBrake`へ移し、
  移動前後の相対パス付きSHA-256集約値が一致した。新版を実起動して
  `state.json`がidleで作られること、新しいアプリログがLibrary側へ作られること、
  Documents側の旧ログディレクトリが再作成されないことを確認した。
- 2026-07-27の指定ジョブログでHandBrake exit code 4と
  `av_interleaved_write_frame ... Input/output error`を確認した。同時刻のmacOS
  統合ログでは`/Volumes/2TB HDD`を含む外部ディスクが取り外され、約10秒後に
  再認識・再マウントされていた。再実行成功とも整合するため、アプリの範囲生成や
  プリセットではなく一時的な外部ストレージ切断が原因と判断した。

## 実ファイル試験

`poc/artifacts/source-four-chapters.mkv`を短い再生成可能fixtureとして使用し、
完成ランナーを通して次を生成した。

- `/private/tmp/chapterbrake-acceptance/統合 Title #01.mkv`
- `/private/tmp/chapterbrake-acceptance/統合 Title #01.mp4`

実行コマンド:

```sh
CHAPTERBRAKE_INTEGRATION=1 \
CHAPTERBRAKE_FIXTURE="$PWD/poc/artifacts/source-four-chapters.mkv" \
CHAPTERBRAKE_ACCEPTANCE_OUTPUT=/private/tmp/chapterbrake-acceptance \
go test ./internal/runner -run TestRealToolchainIntegration -count=1 -v
```

結果:

```text
--- PASS: TestRealToolchainIntegration
    --- PASS: TestRealToolchainIntegration/mkv
    --- PASS: TestRealToolchainIntegration/mp4
```

MKVはH.264映像、入力1由来のAC-3 5.1ch高品質音声、AAC stereo標準音声、
選択した日本語ソフト字幕を保持した。MP4はH.264映像と同じ2音声を保持し、
字幕を含まない。両形式ともtitleは`統合 Title #01`であり、ffprobeによる
後処理前後の構造比較とtitle検証後にだけ最終名へ公開された。

完成MKV/MP4をVLCで直接開き、どちらもプレイリストの表示タイトルが
ファイル名stemの`統合 Title #01`になることを目視確認した。Twonkyへの配置は、
利用者が指定した受け入れ範囲に従い実施していない。

## 実際の主要コマンド

ジョブログへ実行ファイル、引数、開始、終了、結果、生ログパスが記録された。
以下は可読性のためパスだけを短縮した表記である。

MKVエンコード:

```text
HandBrakeCLI --json --preset "H.264 MKV 1080p30" \
  -i source-four-chapters.mkv -o .chapterbrake-...-encode.mkv \
  --chapters 1-2 --markers \
  --audio 1,1 --aencoder copy:ac3,ca_aac --ab 640,160 \
  --mixdown 5point1,stereo --arate auto,auto \
  --subtitle 1 --subtitle-burned=none --subtitle-default=none
```

MKV title:

```text
mkvpropedit .chapterbrake-...-encode.mkv \
  --edit info --set "title=統合 Title #01"
```

MP4エンコード:

```text
HandBrakeCLI --json --preset "Super HQ 1080p30 Surround" \
  -i source-four-chapters.mkv -o .chapterbrake-...-encode.mp4 \
  --chapters 1-2 --markers \
  --audio 1,1 --aencoder copy:ac3,ca_aac --ab 640,160 \
  --mixdown 5point1,stereo --arate auto,auto \
  --subtitle none --subtitle-burned=none --subtitle-default=none
```

MP4 title:

```text
ffmpeg -i .chapterbrake-...-encode.mp4 \
  -map 0:v -map 0:a? -map_metadata 0 -map_chapters 0 \
  -c copy -metadata "title=統合 Title #01" -brand mp42 \
  .chapterbrake-...-metadata.mp4
```

検証:

```text
ffprobe -v error -show_format -show_streams -show_chapters -of json FILE
```

最新実行の要約ログ:

- `/private/tmp/chapterbrake-acceptance/logs/job-20260726T095751.181949000-integration-mkv-統合_Title__01.log`
- `/private/tmp/chapterbrake-acceptance/logs/job-20260726T095752.695573000-integration-mp4-統合_Title__01.log`

## 受け入れ条件A〜L

| 項目 | 結果 | 主な証拠 |
| --- | --- | --- |
| A 起動と保存場所 | PASS | 実アプリ初回起動で設定、空キュー、ログを作成。mode 0600、既定入出力先・区切り時間、version 1・2から3への移行を確認。厳格JSON試験あり |
| B ファイル選択 | PASS | 設定入力先とコマンドライン指定の両方から開始。親を先頭、ディレクトリと通常MKVを名前昇順で表示する単体・TUI画面試験。日本語・空白パスを使用 |
| C チャプター | PASS | 設定可能な区切り時間による近似、画面変更・再計算、単体・出力合計時間、短い末尾結合、実利用21チャプター回帰、全解除、chapter-only引数の表駆動試験。秒・PTS補正なし |
| D 出力名 | PASS | Unicode stem、タイトル名ディレクトリ、既存ファイルとキューを考慮した連番、拡張子、title一致の試験 |
| E 音声 | PASS | 入力1/2の独立選択、高品質+標準品質、AC-3パススルーとAACフォールバックの試験。実出力構造も確認 |
| F 字幕 | PASS | MKVの0/1/複数選択、MP4字幕なし、焼き付け無効引数の試験。実MKV字幕と実MP4字幕なしを確認 |
| G title | PASS | 実MKV/MP4で後処理、構造比較、title検証、VLC表示を確認 |
| H プレビューと既存出力 | PASS | 全ジョブ・除外範囲・衝突一覧、未承認拒否、承認後追加、実行時上書きの試験 |
| I キュー | PASS | 原子的永続化、先頭保持、成功時だけ削除、追加後自動開始、詳細削除、待機ジョブ並び替え、ジョブ境界一時停止、不正JSON fail closedの試験 |
| J 即時中断 | PASS | 実プロセスグループPoCと完成ランナー/TUIのrace付き中断試験。部分出力削除、先頭保持、一時停止、後続停止を確認 |
| K ログ | PASS | アプリ、ジョブ、生stdout/stderrログの試験と実統合ログを確認 |
| L Go品質 | PASS | gofmt、test、race、vet、build、diff check、実ツール統合試験が成功 |

TUI画面回帰試験の追加時に、字幕なし選択がジョブ生成時に`nil`へ変わる問題を
検出した。空のJSON配列を維持するよう修正し、MP4と字幕なしMKVの両方を保護する
テストを追加した。

## 最終検証

次はすべて成功した。

```sh
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
go build ./...
git diff --check
```

通常の`go list ./...`には`poc/`配下のパッケージが現れず、製品の通常ビルド、
テスト、実行はPoCのスクリプト、fixture、生成メディア、ログに依存しない。

## 既知の制約

- HandBrake GUIとCLIで再現するチャプター境界のフレーム混入・欠落は、
  利用者判断に従いHandBrakeの仕様として許容し、補正しない。
- MP4のストリームコピー後にチャプター用dataストリームのlanguageタグが
  `und`から`eng`へ変わることがある。映像・音声・字幕・チャプター内容を維持し、
  音声・字幕のlanguageは厳格比較する。
- 入力音声は初期版ではトラック1・2だけを選択対象とする。
- MP4は初期版では字幕なしとする。

## 受け入れ後の修正確認

2026-07-26に次を追加確認した。

- HandBrakeCLI 1.11.2の`--preset-list`は一覧本体をstderrへ出力する。製品が
  stdout/stderrの両方から標準プリセット一覧を取得し、`Fast 1080p30`を選択して
  MP4と判定できることを実ツール統合試験で確認した。
- `~/.local/bin/chapterbrake`のTUIを実際に操作し、内蔵4件が表示され、
  「その他のプリセットから選ぶ」からHandBrake標準プリセット一覧へ
  エラーなく遷移することを確認した。
- GUIのMy Presets 4件を一対一の内蔵選択肢へ戻した。
- 黒帯付き720x480入力を実エンコードし、`My Old Presets`相当の
  `--crop-mode auto`は720x360、`GCCX`相当の`--crop-mode none`は720x480に
  なることをffprobeで確認した。
- インストール済みTUIを実際に操作し、一覧の上下移動と右キー決定、フォームの
  上下移動、チェック項目の左右切り替え、入力欄内のBackspace文字削除、
  入力欄以外のBackspaceによる前画面遷移を確認した。従来のTabとSpace操作も
  維持している。
- チャプターのチェック項目上でEnterを押し、選択状態を維持したまま音声画面へ
  進むことをインストール済みTUIで確認した。続けて音声画面でEscを一回押し、
  途中画面を経由せずメインメニューへ戻ることを確認した。
- 出力ベース名にフォーカスした状態でEnterを一回押し、開始番号の入力欄や
  「次へ」ボタンへフォーカス移動せず、直接チャプター画面へ進むことを
  インストール済みTUIで確認した。
- version 1の実`settings.json`を起動し、既存`output_directory`を維持したまま、
  version 2と既定`input_directory`へ移行することを確認した。ファイルmode 0600も
  維持された。
- 引数なしのインストール済みTUIでファイル選択が
  `/Volumes/2TB HDD/Images`から始まることを確認した。
- カレントディレクトリ専用の`--cwd`は、任意ディレクトリを指定できる
  `--directory PATH`と短縮形`-d PATH`へ置き換えた。`poc/artifacts`から
  `chapterbrake -d .`を起動し、同ディレクトリからファイル選択が始まることと、
  `settings.json`の既定入力先が変更されないことを確認した。
- 実`settings.json`がversion 3となり、`chapter_interval: "23:40"`を保持することを
  確認した。version 1・2から既存入出力先を維持して移行する単体試験も追加した。
- インストール済みTUIで区切り時間入力が出力名画面ではなくチャプター分割画面の
  先頭にあることを確認した。`23:40`を`45:00`へ変更してEnterを押すと、画面タイトルが
  `45:00近似: 有効`へ更新され、近似選択が再計算された。
- 画面を終了した後も実`settings.json`の`chapter_interval`が`23:40`のままで、
  一時変更が設定へ書き戻されないことを確認した。
- ファイル選択一覧を`../`先頭、大文字小文字を区別しない名前昇順へ変更した。
  インストール済みTUIを`poc/artifacts`で開き、`audio-preset-rule-match.mkv`、
  `audio-preset-rule-order.mkv`、`audio-quality-fallback.mkv`の順に並ぶことを
  確認した。
- 実`settings.json`をversion 3の旧既定出力先から起動し、mode 0600を維持したまま
  version 4、`output_directory: "/Volumes/2TB HDD/Movies"`へ移行することを確認した。
- 一時HOMEの空キューでインストール済みTUIを起動し、出力名画面に新しい出力先が
  表示されることを確認した。`source-four-chapters.mkv`の末尾は3秒だったため、
  末尾除外欄が`chapter 004 / 0:03`かつオフで表示された。
- 同じ画面の`23:40`入力欄でEnterを押し、同じチャプター画面へ留まらず音声画面へ
  進むことを確認した。キュー画面には`Delete/d:削除`が表示された。
- 単体試験では、2秒ちょうどまでの末尾チャプターを自動除外し、2秒超と
  単一チャプターは除外しないこと、解除操作と最終チャプター手動チェック、
  プレビューの末尾除外範囲を確認した。
- 実行中の画面バックグラウンド化、実行状況への復帰、終了確認、待機ジョブ削除、
  実行中追加の後続処理、実行中先頭削除拒否、単一起動lockを通常テストと
  race detectorで確認した。
