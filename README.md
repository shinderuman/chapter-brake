# ChapterBrake

ChapterBrakeは、macOS上でMKVをチャプター番号の範囲ごとに分割し、
HandBrakeCLIの永続キューとして順次処理するGo製TUIアプリケーションです。

各入力音声トラックから高品質版と標準品質版を作り、字幕は焼き付けずに扱います。
完成ファイルのtitleメタデータはファイル名へ揃えるため、VLCやDLNAクライアントで
連番どおり識別できます。

## 必要環境

- macOS
- Go 1.26系
- HandBrakeCLI 1.11系
- FFmpeg / ffprobe 8系
- MKVToolNix（mkvpropedit）

Homebrewでの導入例:

```sh
brew install go handbrake ffmpeg mkvtoolnix
```

HandBrake GUIからエクスポートした`My Presets.json`を
`~/Documents/ChapterBrake/My Presets.json`へ置くと、その中のプリセットを
既定一覧へ表示してHandBrakeCLIへ明示的に読み込ませます。GUIアプリ自体や
GUI内部設定には依存しません。ファイルがない場合だけ、従来の
`MP4 Presets`、`MKV Presets`、`My Old Presets`、`GCCX`相当の4件を
互換一覧として表示します。
「その他のプリセットから選ぶ」ではHandBrakeCLIの標準プリセットだけを表示します。

## ビルドと起動

`make`するとビルド後に`~/.local/bin/chapterbrake`へインストールします。

```sh
make
chapterbrake
```

引数なしでは`settings.json`の`input_directory`からファイル選択を開始します。
既定値は`/Volumes/2TB HDD/Images`です。その一回だけ別のディレクトリから
選びたい場合は、設定を書き換えずに`--directory`または短縮形`-d`で指定します。
相対パスも使用でき、カレントディレクトリなら`.`を指定します。

```sh
chapterbrake --directory /path/to/videos
chapterbrake -d .
```

`~/.local/bin`が`PATH`に入っていない場合は、使用しているシェルの設定へ
次を追加してください。

```sh
export PATH="$HOME/.local/bin:$PATH"
```

ビルドだけを行う場合は`make build`、配置先を変更する場合は
`make BINDIR=/absolute/path/to/bin`を使用できます。

初回起動時に次を作成します。

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

初期入力先は`/Volumes/2TB HDD/Images`、初期出力先は
`/Volumes/2TB HDD/Movies`、初期区切り時間は`23:40`です。設定画面は
ありません。恒久的に変更する場合はアプリ停止中に`settings.json`の
`input_directory`、`output_directory`、`chapter_interval`を編集してください。
区切り時間は`分:秒`形式で、ジョブ追加時のチャプター分割画面でもその回だけ
変更できます。
version 1・2の既存設定は既存値を維持してversion 4へ移行します。version 3で
旧既定出力先`/Volumes/2TB HDD/mp4/`のままなら新既定値へ移行し、変更済みの
出力先は維持します。
不正JSON、未知のversion、不正な区切り時間、存在しない入力先、
存在しない入力先、または存在しない・書き込めない出力先の基準ディレクトリは
自動修復せずエラーで停止します。タイトル名の子ディレクトリはジョブ開始時に
必要なら作成します。

## 操作の流れ

1. 「新しいジョブを追加」を選ぶ。
2. ディレクトリを移動し、通常のMKVファイルを一つ選ぶ。
3. 既定プリセット、または「その他」からHandBrake標準プリセットを選ぶ。
4. 出力ベース名と開始番号を確認する。
5. 区切り時間と末尾短チャプターの除外を確認し、出力開始チャプターを選ぶ。
6. 入力音声トラック1・2から一つ以上を選ぶ。
7. MKVの場合だけ、格納するソフト字幕を選ぶ。
8. 全出力名、チャプター範囲、音声、字幕、titleをプレビューする。
9. 既存出力がある場合は、一覧を確認して上書きを承認する。
10. キューへ追加すると、一時停止中でなければ順次処理を開始する。

主なキー:

- `↑` / `↓`: 項目移動
- `→` / `Enter`: 一覧の決定
- `←` / `→` / `Space`: チェック切り替え
- チェック項目上の`Enter`: チェックを変えず次へ
- 出力名・開始番号入力中の`Enter`: 入力を確定して次へ
- チャプター画面の区切り時間入力中の`Enter`: 入力値で再計算して次へ
- `←` / `Backspace`: 多くの設定画面で前の画面へ戻る
- キュー追加中の`Esc`: メインメニューへ戻る
- 入力欄では`←` / `→`がカーソル移動、`Backspace`が文字削除
- ファイル選択では`../`の決定が親ディレクトリ移動、`Backspace` / `Esc`がメインへ戻る
- キュー一覧の`Enter`: ジョブ詳細を表示
- キュー一覧の`j` / `k`: 待機ジョブを一段下／上へ並び替え
- 実行中ジョブ詳細: エンコード一時停止・再開、現在ジョブ後の一時停止、
  または即時中断して一時停止

実行中も同じアプリでキューへ追加できます。キュー一覧には進捗・段階・ETAを表示し、
各ジョブの参考動画時間と全体ETAも表示します。メイン画面にも全キューを
読み取り専用で表示します。待機ジョブの詳細から削除でき、`j` / `k`を繰り返すと
同じジョブを連続して下／上へ移動できます。実行中ジョブは削除・移動できません。
二重起動と複数同時エンコードは行いません。

## チャプター、音声、字幕

切り出し範囲にはHandBrakeCLIの`--chapters N-M`だけを使います。秒、フレーム、
PTS指定やエンコード後の境界補正は行いません。HandBrake GUIでも発生する
素材依存の境界重複・欠落はHandBrakeの仕様として扱います。
最終チャプターが2秒以下なら、チャプター画面の「末尾の短いチャプターを除外」を
初期オンにします。ユーザーが解除すれば通常どおり最終チャプターも出力します。

チャプター画面には動画全体、各チャプターの開始・単体時間、各チェック位置から
生成される出力合計時間を表示します。自動近似で短い末尾単体出力が生じる場合は、
直前の最終区間へ結合して画面先頭に範囲と合計時間を表示します。

音声ビットレートの数値入力はありません。選択した各入力トラックから次を作ります。

- 高品質: PoC済みのAC-3はパススルー。それ以外は高品質AACへフォールバック。
- 標準品質: AAC stereo。

初期版で選べる入力音声はトラック1・2です。トラック3以降は表示しても
エンコード対象にはしません。

MKVでは選択字幕をソフト字幕として格納できます。MP4は字幕なしです。
どちらもHandBrakeの自動字幕選択と焼き付けを明示的に無効化します。

## キュー、上書き、中断

`queue.json`の配列順に一件ずつ実行します。先頭ジョブは、エンコード、title設定、
ffprobe検証、最終rename、queue保存が成功するまで残ります。
キュー画面からメインへ戻っても処理は継続し、その間に同じアプリから追加した
ジョブも後続として処理します。

キュー追加時に承認した既存出力は、ジョブ開始時に確認なしで削除します。
完成ファイルとHandBrakeCLIのジョブ固有一時ファイルは、
`<出力先>/<出力ベース名>/`へ書きます。同名ディレクトリは再利用します。

「即時中断して一時停止」では、外部プロセスグループへSIGINTを送り、終了しなければ
期限後にSIGKILLします。プロセスの終了を待ってから部分出力を削除し、キュー先頭を
残して後続へ進みません。通常終了は現在ジョブを完了してから残りを一時停止します。
次回起動後は残った先頭ジョブから再開します。中断した同一ジョブは最初から処理します。

実行中詳細の「エンコードを一時停止」はHandBrakeCLIのプロセスグループを
`SIGSTOP`で止め、「再開」で`SIGCONT`を送ります。同じアプリを開いている間だけ
同じ処理を再開でき、アプリ終了をまたぐレジュームではありません。

## title後処理と表示確認

- MKV: `mkvpropedit`で一時MKVのsegment titleだけを直接変更します。
- MP4: `ffmpeg -c copy`で映像・音声を再エンコードせず、別の一時MP4へtitleを設定します。
- `ffprobe`でtitle、ストリーム、チャプター、時刻構造を確認した後だけ最終名へrenameします。

title値は常に最終ファイル名から拡張子を除いた文字列です。Twonkyへ配置する前の
確認は、完成MKV/MP4をVLCで直接開き、表示名がファイル名のstemと一致することを
確認すれば十分です。

## ログ

`~/Library/Logs/ChapterBrake/`へ次を保存します。

- 日別アプリログ
- ジョブ要約ログ
- HandBrakeCLI、ffmpeg、ffprobe、mkvpropeditごとのstdout/stderr生ログ

アプリログには実際に使用する各ツールの絶対パスとバージョンを記録します。
ジョブ失敗・中断時のTUIエラーには段階とジョブログのパスが含まれます。

ログの自動削除・圧縮・ローテーションは行いません。

## 開発時の検証

通常検証:

```sh
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

PoCの短いMKVと実ツールを使うランナー統合試験:

```sh
CHAPTERBRAKE_INTEGRATION=1 \
CHAPTERBRAKE_FIXTURE=/absolute/path/to/source-four-chapters.mkv \
go test ./internal/runner -run TestRealToolchainIntegration -v -count=1
```

GUIエクスポートと実HandBrakeCLIで一時停止・再開を確認する試験:

```sh
CHAPTERBRAKE_INTEGRATION=1 \
CHAPTERBRAKE_FIXTURE="$PWD/poc/artifacts/source-long-chapters.mkv" \
CHAPTERBRAKE_PRESET_FILE="$HOME/Documents/ChapterBrake/My Presets.json" \
go test ./internal/process -run TestRealHandBrakePauseResume -v -count=1
```

PoC固有のスクリプトとfixtureは`poc/`の独立Goモジュールへ隔離され、製品の
ビルド・通常テスト・実行時依存には含まれません。成立根拠は
`docs/POC_RESULT.md`と`docs/LOCAL_INVESTIGATION.md`にあります。

## ライセンス

MIT Licenseです。詳細は[LICENSE](LICENSE)を参照してください。
