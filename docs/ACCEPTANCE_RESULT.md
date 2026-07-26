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
`~/Documents/ChapterBrake/logs/app-2026-07-26.log`へ記録されることも確認した。

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
| A 起動と保存場所 | PASS | 実アプリ初回起動で設定、空キュー、ログを作成。mode 0600、既定入出力先、version 1から2への移行を確認。厳格JSON試験あり |
| B ファイル選択 | PASS | 設定入力先と`--cwd`の両方から開始。ディレクトリ、親、通常MKVだけをOS列挙順で表示する単体・TUI画面試験。日本語・空白パスを使用 |
| C チャプター | PASS | 範囲生成、23分40秒近似、経過時間、全解除、chapter-only引数の表駆動試験。秒・PTS補正なし |
| D 出力名 | PASS | Unicode stem、既存ファイルとキューを考慮した連番、拡張子、title一致の試験 |
| E 音声 | PASS | 入力1/2の独立選択、高品質+標準品質、AC-3パススルーとAACフォールバックの試験。実出力構造も確認 |
| F 字幕 | PASS | MKVの0/1/複数選択、MP4字幕なし、焼き付け無効引数の試験。実MKV字幕と実MP4字幕なしを確認 |
| G title | PASS | 実MKV/MP4で後処理、構造比較、title検証、VLC表示を確認 |
| H プレビューと既存出力 | PASS | 全ジョブ・除外範囲・衝突一覧、未承認拒否、承認後追加、実行時上書きの試験 |
| I キュー | PASS | 原子的永続化、先頭保持、成功時だけ削除、失敗停止、不正JSON fail closedの試験 |
| J 即時中断 | PASS | 実プロセスグループPoCと完成ランナー/TUIのrace付き中断試験。部分出力削除、先頭保持、後続停止を確認 |
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
- `poc/artifacts`から`chapterbrake --cwd`を起動し、同ディレクトリからファイル
  選択が始まることと、`settings.json`の既定入力先が変更されないことを確認した。
