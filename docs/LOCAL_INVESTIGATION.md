# ローカル実機調査

調査日: 2026-07-26

## 1. 結論

HandBrakeCLI 1.11.2とHandBrake GUI 1.11.2でチャプター番号だけを指定した
非先頭チャプターの出力に、選択開始チャプターより前の映像が
30フレーム（1秒）混入した。

- 3秒チャプターのチャプター2単独: 期待90フレーム、実測121フレーム
- 30秒チャプターのチャプター2単独（CLI）: 期待900フレーム、実測931フレーム
- 30秒チャプターのチャプター2単独（GUI）: 期待900フレーム、実測930フレーム
- CLIとGUIのログでは、選択した開始チャプターがどちらも出力フレーム31で始まる

この結果はHandBrake GUIでも再現し、従来のGUI運用で実害がなかったため、
HandBrake固有の挙動として許容する。時刻・PTS指定や後処理による自動補正は行わない。
変更後のP-03は合格とする。

タイトル表示は完成MKV/MP4をVLCで直接開いて確認した。音声は任意ビットレート
入力を廃止し、各選択入力からプリセット由来の高品質・標準品質を生成する方式を
実エンコードで確認した。最終判定は`GO`。

## 2. 実行環境

完全な出力: `poc/logs/environment-versions.txt`

| 項目 | 実測 |
|---|---|
| macOS | 26.5.2 (25F84), arm64 |
| CPU | Apple M1、論理8コア |
| Go | `/opt/homebrew/bin/go`, go1.26.5 darwin/arm64 |
| HandBrakeCLI | `/opt/homebrew/bin/HandBrakeCLI`, 1.11.2 (2026060700) |
| FFmpeg / ffprobe | `/opt/homebrew/bin`, 8.1.2 |
| mkvpropedit / mkvmerge | `/opt/homebrew/bin`, MKVToolNix 100.0 |
| HandBrake GUI | `/Applications/HandBrake.app` |

HandBrakeCLIとMKVToolNixはPoC開始時に未導入だったためHomebrewで導入した。
その際、既存FFmpeg 7.0.1と更新済み`libbluray`等のABI不整合が発生したため、
FFmpegを8.1.2へ更新して解消した。失敗ログは
`poc/logs/generate-fixture.stderr.log`へ残っている。

HandBrakeCLIの実環境ヘルプは`poc/logs/handbrake-help.txt`、組み込みプリセット
一覧は`poc/logs/handbrake-preset-list.txt`、GUI取り込み後の一覧は
`poc/logs/handbrake-gui-preset-list.txt`へ保存した。

## 3. GUIカスタムプリセット

GUIプリセットの正本は次に存在した。

```text
~/Library/Containers/fr.handbrake.HandBrake/Data/Library/Application Support/HandBrake/UserPresets.json
```

CLIからは次で取り込めた。

```sh
HandBrakeCLI --preset-import-gui --preset-list
```

`My Presets`配下に次の実プリセットが存在した。

| プリセット | `FileFormat` | `VideoEncoder` |
|---|---|---|
| MP4 Presets | `av_mp4` | `x264` |
| MKV Presets | `av_mkv` | `x264` |
| My Old Presets | `av_mp4` | `x264` |
| GCCX | `av_mp4` | `x264` |

名前文字列ではなく、プリセットJSONの`FileFormat`を再帰走査すれば
`av_mp4` / `av_mkv`を確定できる。CLIから選択したプリセットをエクスポートした
結果でも同じフィールドを確認した。

```sh
HandBrakeCLI --preset-import-gui --preset "MP4 Presets" \
  --preset-export "poc-probe-mp4" \
  --preset-export-file poc/artifacts/preset-mp4.json
HandBrakeCLI --preset-import-gui --preset "MKV Presets" \
  --preset-export "poc-probe-mkv" \
  --preset-export-file poc/artifacts/preset-mkv.json
```

実出力はffprobeでMP4が`mov,mp4,m4a,3gp,3g2,mj2`、MKVが
`matroska,webm`であることを確認した。

## 4. 検証素材

再生成:

```sh
./poc/generate_fixture.sh
```

生成物`poc/artifacts/source-four-chapters.mkv`の仕様:

- 320x180、30fps、12秒
- H.264映像
- 3秒ずつ赤、緑、青、黄の4チャプター
- 音声1: 日本語、AC-3 5.1ch 640 kbps、440Hz
- 音声2: 英語、AC-3 5.1ch 640 kbps、880Hz
- 字幕1: 日本語SRT
- 字幕2: 英語SRT
- 元title: `PoC Source Original Title`

素材のffprobe JSONは`poc/logs/source-four-chapters.ffprobe.json`。
長いチャプターの再試験用に、同素材をストリームコピーで120秒へ反復し、
`poc/fixture/chapters-long.ffmeta`の30秒×4チャプターを付けた
`source-long-chapters.mkv`も生成した。

## 5. 入力解析と番号対応

実行:

```sh
HandBrakeCLI --json \
  -i poc/artifacts/source-four-chapters.mkv \
  --scan
```

完全ログ:

- `poc/logs/handbrake-scan.stdout.log`
- `poc/logs/handbrake-scan.stderr.log`

HandBrake JSONスキャンは次を返した。

| HandBrake番号 | 種別 | 識別情報 |
|---|---|---|
| 1 | 音声 | jpn, `Tone 440Hz`, AC-3 5.1ch |
| 2 | 音声 | eng, `Tone 880Hz`, AC-3 5.1ch |
| 1 | 字幕 | jpn, `Japanese Test`, UTF-8 |
| 2 | 字幕 | eng, `English Test`, UTF-8 |

ffprobeのストリームindexは映像0、音声1/2、字幕3/4であり、HandBrake番号とは
同一概念でない。製品で使う場合はHandBrake JSONの`TrackNumber`を正本とする。

音声出力のゼロ交差率は440Hz側が約0.0184、880Hz側が約0.0367であり、
HandBrake番号1/2と内容の対応を実出力で確認した。

## 6. 実エンコード引数

全ケースは`./poc/run_encodes.sh`で再実行できる。共通部分:

```sh
HandBrakeCLI \
  --json \
  --preset-import-gui \
  --preset "MKV Presets" \
  -i poc/artifacts/source-four-chapters.mkv \
  -o <一時出力> \
  --chapters <開始-終了> \
  --markers
```

音声:

```text
音声1のみ640: --audio 1 --aencoder ac3 --ab 640 --mixdown 5point1
音声2のみ640: --audio 2 --aencoder ac3 --ab 640 --mixdown 5point1
音声1+2:      --audio 1,2 --aencoder ac3,ac3 --ab 640,160 \
              --mixdown 5point1,stereo
```

字幕なし・非焼き付け:

```text
--subtitle none --subtitle-burned=none --subtitle-default=none
```

字幕1または1+2をソフト格納:

```text
--subtitle 1 --subtitle-burned=none --subtitle-default=none
--subtitle 1,2 --subtitle-burned=none --subtitle-default=none
```

GUIプリセットは`SubtitleAddForeignAudioSearch=true`かつ
`SubtitleBurnBehavior=foreign`だったが、上記の明示引数で上書きできた。

## 7. チャプター境界

解析再実行:

```sh
./poc/analyze_chapter_boundaries.sh
```

結果: `poc/logs/chapter-boundary-analysis.tsv`

| 出力 | 期待 | 実測 | 混入 |
|---|---:|---:|---|
| chapter 1-2 | 180フレーム | 181 | 次チャプター先頭1フレーム |
| chapter 2 | 90フレーム | 121 | 直前30 + 次先頭1 |
| chapter 3-4 | 180フレーム | 210 | 直前30 |
| 30秒chapter 2（CLI） | 900フレーム | 931 | 直前30 + 次先頭1 |
| 30秒chapter 2（GUI） | 900フレーム | 930 | 直前30 + 次先頭1、選択区間内1フレーム欠落 |

`range-2.mkv`は最初の30フレームが前チャプターの赤、その後90フレームが
選択チャプターの緑、末尾1フレームが次チャプターの青だった。

30秒チャプターのログ:

```text
sync: expecting 900 video frames
sync: "Long 2" (2) at frame 31 time 90000
sync: "Long 3" (3) at frame 931 time 2790420
sync: got 931 frames, 900 expected
```

HandBrake GUIを実際に操作し、同じ`source-long-chapters.mkv`、同じ
`MKV Presets`、範囲「チャプター2から2」で
`gui-long-range-2.mkv`を生成した。GUIの画面表示は長さ`00:00:30`だったが、
ffprobeでは31.004秒、930フレームだった。

GUIアクティビティログ:

```text
sync: expecting 900 video frames
sync: "Long 2" (2) at frame 31 time 90000
sync: video time went backwards 33 ms, dropped 1 frames. PTS 1785000
sync: "Long 3" (3) at frame 930 time 2787420
sync: got 930 frames, 900 expected
```

デコード後の色をフレーム単位で調べると、GUI出力の1〜30フレームは
直前チャプター由来の緑、31フレーム目から選択チャプター由来の青だった。
930フレーム目には次チャプター由来の赤が入り、選択区間内ではログどおり
1フレームが欠落していた。完全ログと機械解析は次に保存した。

- `poc/logs/gui-long-range-2.activity.log`
- `poc/logs/gui-long-range-2.ffprobe.json`
- `poc/logs/gui-cli-boundary-analysis.tsv`
- `poc/logs/gui-boundary-runs.tsv`

このため、開始前30フレームの混入はHandBrakeCLIだけの挙動ではなく、
HandBrake GUIでも再現する。

HandBrake公式のPoint to Point Encoding文書にも正確なフレーム精度は保証されない
旨がある。GUIと同等の境界挙動を許容し自動補正しない要件へ変更したため、
この記録を根拠にP-03を合格とする。

## 8. 音声

ffprobe結果:

| ケース | 出力 |
|---|---|
| 音声1のみ | jpn AC-3 5.1ch 640000 bit/s |
| 音声2のみ | eng AC-3 5.1ch 640000 bit/s |
| 音声1+2 | jpn AC-3 5.1ch 640000 + eng AC-3 stereo 160000 bit/s |

完全なffprobe JSONは`poc/logs/audio-*.ffprobe.json`。
HandBrake stderrにも入力番号、名称、出力エンコーダ、mixdown、bitrateが保存されている。

追加検証で、GUIプリセットの音声ルールを単純に出力順へ割り当てる方法は
要求ビットレートを維持しないことが判明した。`MKV Presets`のルールは順に
AAC 160 kbps stereo、AC-3 passthrough 640 kbps 5.1chである。

```text
--audio 1,2
--aencoder ca_aac,copy:ac3
--ab 640,160
```

この指定ではAAC 640が320 kbpsへ自動変更され、AC-3 passthroughへ指定した
160 kbpsは無視されて元の640 kbpsになった。

一方、要求ビットレートと同じプリセットルールへ対応付けた次の指定は成立した。

```text
--audio 1,2
--aencoder copy:ac3,ca_aac
--ab 640,160
--mixdown 5point1,stereo
--arate auto,auto
```

出力はjpn AC-3 5.1ch 640000 bit/sとeng AAC stereo 160 kbps指定になった。
証拠は`audio-preset-rule-match.*`と`audio-preset-rule-order.*`。

利用者の目的は数値指定ではなく、品質の高い音声と標準品質の音声を作ることと
確定したため、ビットレート入力を廃止した。選択した各入力トラック番号を2回並べ、
プリセットのパススルー音声ルールを高品質、非パススルー音声ルールを標準品質として
明示する方式を試験した。

| 入力選択 | 出力 |
|---|---|
| トラック1 | jpn AC-3 5.1ch + jpn AAC stereo |
| トラック2 | eng AC-3 5.1ch + eng AAC stereo |
| トラック1+2 | 上記4音声を入力順・品質順で格納 |
| トラック1、MP4プリセット | jpn AC-3 5.1ch + jpn AAC stereo |

パススルー非対応のAAC入力では、`copy:ac3`をそのまま渡すと高品質側が削除された。
スキャン結果に基づき高品質側をプリセットのフォールバック`ca_aac`へ明示変更すると、
高品質AAC 5.1chと標準品質AAC stereoの2音声を生成できた。

再実行:

```sh
./poc/run_audio_quality_poc.sh
./poc/verify_audio_quality.sh
```

結果は`poc/logs/audio-quality-verification.tsv`。入力1/2/1+2、明示フォールバック、
MP4プリセット、入力音声1本だけのスキャンをすべて確認したためP-04を合格とする。

## 9. 字幕と非焼き付け

`mkv-no-subtitles.mkv`、`mkv-subtitle-1.mkv`、
`mkv-subtitles-1-2.mkv`を同一映像設定で生成した。

- 字幕数は順に0、1、2
- 格納形式はSRT (`subrip`)
- MP4の字幕数は0
- 4出力すべてのデコード後映像フレームSHA-256:
  `04b6ec6512b36ade5c06d8ebcc1365b1b6b5a02856374af18ae9b61b36358374`
- 映像パケット、デコード音声、音声パケットのSHA-256も全ケースで一致

再検証:

```sh
./poc/verify_media.sh
```

結果は`poc/logs/media-verification.tsv`。字幕表示時間を含む全フレームが一致するため、
焼き付けは発生していない。

## 10. title後処理

### MKV

```sh
mkvpropedit <encode-temp.mkv> \
  --edit info \
  --set "title=日本語 Title #01"
```

小型素材では約0.05秒。前後で次が一致した。

- 映像・音声・字幕パケットハッシュ
- デコード後映像・音声ハッシュ
- ストリーム数、コーデック、time base、start/duration
- 4チャプター

2,181,667,448 bytesの実利用MKVをCodexVaultへコピーし、コピー元とサイズ・
SHA-256を照合してからコピー側だけを編集した。mkvpropeditは0.07秒で完了し、
編集前後でファイルサイズ、5ストリーム、4チャプター、全パケットSHA-256が一致した。

大容量コピー:

```text
/Volumes/CodexVault/chapter-brake-poc/large-title-test.mkv
```

コピー時SHA-256:

```text
f7f701fb769016608341c17360ad97305cea7491e71bc723296cc2d0942538dd
```

### MP4

最初に試した次の引数は不合格だった。

```text
-map 0 -map_metadata 0 -map_chapters 0 -c copy
```

HandBrake MP4のチャプター用`bin_data`を`-map 0`がコピーし、さらに
`-map_chapters 0`がデータストリームを作るため、1本が2本へ増えた。

確定した引数:

```sh
major_brand=$(ffprobeで入力のmajor_brandを取得)
ffmpeg -i <encode-temp.mp4> \
  -map 0:v -map "0:a?" \
  -map_metadata 0 -map_chapters 0 \
  -c copy \
  -metadata "title=<出力stem>" \
  -brand "$major_brand" \
  <metadata-temp.mp4>
```

修正後は映像1、音声1、チャプター用data 1、4チャプターを維持した。
映像・音声のパケットハッシュとデコードハッシュは一致し、start_time、
duration、time baseも一致した。ffmpeg muxerによりグローバル`encoder`タグは
HandBrakeから`Lavf`へ変わるが、必要ストリーム、内容、同期には変化がない。

`./poc/verify_metadata.sh`はtitle検証後だけ、日本語・空白・`#`を含む最終名へ
同一ディレクトリでrenameする。検証前に最終名は存在しない。

利用者判断により、Twonky配置とDLNA経由の表示確認は必須とせず、完成ファイルを
VLCで直接開く確認へ変更した。次の2ファイルを実際に開き、ウインドウタイトルと
画面内表示が拡張子を除いたファイル名と一致した。

- `日本語 Title #01.mkv` → `日本語 Title #01`
- `日本語 Title #02.mp4` → `日本語 Title #02`

画面証拠は`poc/logs/vlc-mkv-title.jpeg`と`poc/logs/vlc-mp4-title.jpeg`。
変更後のP-06は合格とする。

## 11. 既存出力

HandBrakeCLI 1.11.2は既存出力を指定しても終了コード0で上書きした。
この挙動には依存せず、ジョブ開始前にアプリが既存出力を削除する設計を維持する。

書き込み不可ディレクトリ内の既存ファイル削除は`Permission denied`、終了コード1
となった。削除結果を確認してからHandBrakeCLIを起動すれば、削除不能時に
エンコードを開始しない経路を実装できる。

## 12. Goからの即時中断

最小スパイク: `poc/interrupt.go`

- 子コマンドを新しいプロセスグループで起動
- プロセスグループへSIGINT
- 猶予後も残る場合はSIGKILL
- `Wait`完了後だけ部分出力を削除
- stdout/stderrを別ファイルへ完全保存しながら端末へtee

| コマンド | 停止結果 | 停止時間 | 部分出力 |
|---|---|---:|---|
| HandBrakeCLI | SIGINTを処理してexit 1 | 293ms | Wait後削除 |
| ffmpeg | SIGINTを処理してexit 255 | 2ms | Wait後削除 |
| mkvpropedit | SIGINTでsignaled | 0ms未満 | Wait後削除 |

すべて`process_group_gone=true`で、SIGKILLへの昇格は不要だった。
中断した同一HandBrake引数を部分出力削除後に再実行し、12秒の完成MKVを
正常生成できた。

結果:

- `poc/logs/interrupt-handbrake.result.json`
- `poc/logs/interrupt-ffmpeg.result.json`
- `poc/logs/interrupt-mkvpropedit.result.json`
- `poc/logs/restart-interrupt.result.json`

## 13. ログと進捗

Goスパイクのtee前後SHA-256を比較し、3コマンドすべてで保存ログと端末出力が
一致した。結果は`poc/logs/log-tee-verification.txt`。

HandBrakeCLIの`--json` stdoutから中断前に12個の`Progress`イベントを取得した。
stderrは完全保存した。JSON進捗はTUI表示へ使え、パース不能時も生ログ保存を
継続する設計が可能である。全出力をメモリへ蓄積せず`io.MultiWriter`で
ストリーム処理できた。

## 14. TUIライブラリ

比較対象:

- Bubble Tea v2 / Bubbles v2: 活発に保守され、Elm型更新モデルとlist/textinput等を持つ
- tview v0.42.0: List、Checkbox、InputField、Pages、入力捕捉を直接持つ

初期版の画面要件を少ないコードで表現できるため、PoC候補としてtviewを選んだ。
`poc/tui-spike`で次を実装した。

- リスト選択
- 複数チェック
- 日本語を含むテキスト入力
- ページ遷移
- Ctrl+C捕捉と停止

検証:

```sh
cd poc/tui-spike
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

すべて成功した。製品採用はMilestone 0がGOになった後に再確認する。

## 15. 主要な失敗と未確認

1. FFmpeg 7.0.1はHomebrew依存更新後にABI不整合となり8.1.2へ更新した。
2. チャプター番号範囲だけの出力に30フレームの開始前映像が混入したが、
   GUIでも同じため許容し自動補正しない方針へ変更した。
3. MP4の`-map 0`はチャプター用dataを重複させたため、映像・音声の明示mapへ変更した。
4. DLNA経由ではなくVLC直接表示を必須証拠とする方針へ変更し、MKV/MP4とも確認した。
5. 音声を高品質・標準品質の2ルールへ変更し、全選択パターンとフォールバックを確認した。
