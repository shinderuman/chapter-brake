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

HandBrake GUIは不要です。既定一覧にはGUIのMy Presetsに対応する
`MP4 Presets`、`MKV Presets`、`My Old Presets`、`GCCX`の4件を
ChapterBrake側で定義し、HandBrakeCLIの標準プリセットを土台にします。
低解像度の`My Old Presets`は自動クロップ、`GCCX`はクロップなしです。
「その他のプリセットから選ぶ」ではHandBrakeCLIの標準プリセットだけを表示します。

## ビルドと起動

`make`するとビルド後に`~/.local/bin/chapterbrake`へインストールします。

```sh
make
chapterbrake
```

`~/.local/bin`が`PATH`に入っていない場合は、使用しているシェルの設定へ
次を追加してください。

```sh
export PATH="$HOME/.local/bin:$PATH"
```

ビルドだけを行う場合は`make build`、配置先を変更する場合は
`make BINDIR=/absolute/path/to/bin`を使用できます。

起動ディレクトリがファイル選択の初期位置になります。日本語・空白を含むパスも
そのまま扱います。

初回起動時に次を作成します。

```text
~/Documents/ChapterBrake/
├── settings.json
├── queue.json
└── logs/
```

初期出力先は`/Volumes/2TB HDD/mp4/`です。初期版に設定画面はありません。
変更する場合はアプリ停止中に`settings.json`の`output_directory`を絶対パスで
編集してください。不正JSONや存在しない・書き込めない出力先は自動修復せず、
エラーで停止します。

## 操作の流れ

1. 「新しいジョブを追加」を選ぶ。
2. ディレクトリを移動し、通常のMKVファイルを一つ選ぶ。
3. 既定プリセット、または「その他」からHandBrake標準プリセットを選ぶ。
4. 出力ベース名と開始番号を確認する。
5. 23分40秒近似の初期チェックを調整し、出力開始チャプターを選ぶ。
6. 入力音声トラック1・2から一つ以上を選ぶ。
7. MKVの場合だけ、格納するソフト字幕を選ぶ。
8. 全出力名、チャプター範囲、音声、字幕、titleをプレビューする。
9. 既存出力がある場合は、一覧を確認して上書きを承認する。
10. キューへ追加し、メインメニューの「キューを実行」から順次処理する。

主なキー:

- `↑` / `↓`: 項目移動（一覧では`j` / `k`も利用可能）
- `→` / `Enter`: 一覧の決定
- `←` / `→` / `Space`: チェック切り替え
- チェック項目上の`Enter`: チェックを変えず次へ
- 出力名・開始番号入力中の`Enter`: 入力を確定して次へ
- `←` / `Backspace`: 前の画面へ戻る
- キュー追加中の`Esc`: メインメニューへ戻る
- 入力欄では`←` / `→`がカーソル移動、`Backspace`が文字削除
- ファイル選択では`←`が親ディレクトリ、`Backspace` / `Esc`がメインへ戻る
- 実行中の`Ctrl+C`: 現在ジョブを即時中断

キュー表示は読み取り専用です。編集、並べ替え、複数同時エンコードは行いません。

## チャプター、音声、字幕

切り出し範囲にはHandBrakeCLIの`--chapters N-M`だけを使います。秒、フレーム、
PTS指定やエンコード後の境界補正は行いません。HandBrake GUIでも発生する
素材依存の境界重複・欠落はHandBrakeの仕様として扱います。

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

キュー追加時に承認した既存出力は、ジョブ開始時に確認なしで削除します。
HandBrakeCLIは最終名へ直接書かず、同じ出力ディレクトリのジョブ固有一時ファイルへ
書きます。

実行中の`Ctrl+C`では、外部プロセスグループへSIGINTを送り、終了しなければ
期限後にSIGKILLします。プロセスの終了を待ってから部分出力を削除し、キュー先頭を
残します。次回実行時は同じジョブを最初から処理します。

## title後処理と表示確認

- MKV: `mkvpropedit`で一時MKVのsegment titleだけを直接変更します。
- MP4: `ffmpeg -c copy`で映像・音声を再エンコードせず、別の一時MP4へtitleを設定します。
- `ffprobe`でtitle、ストリーム、チャプター、時刻構造を確認した後だけ最終名へrenameします。

title値は常に最終ファイル名から拡張子を除いた文字列です。Twonkyへ配置する前の
確認は、完成MKV/MP4をVLCで直接開き、表示名がファイル名のstemと一致することを
確認すれば十分です。

## ログ

`~/Documents/ChapterBrake/logs/`へ次を保存します。

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

PoC固有のスクリプトとfixtureは`poc/`の独立Goモジュールへ隔離され、製品の
ビルド・通常テスト・実行時依存には含まれません。成立根拠は
`docs/POC_RESULT.md`と`docs/LOCAL_INVESTIGATION.md`にあります。

## ライセンス

MIT Licenseです。詳細は[LICENSE](LICENSE)を参照してください。
