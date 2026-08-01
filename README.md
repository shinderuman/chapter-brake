# ChapterBrake

ChapterBrakeは、macOSのローカルWeb UIからMKVをチャプター番号範囲ごとに分割し、
HandBrakeCLIの永続キューとして順次処理するGoアプリケーションです。

各入力音声トラックから高品質版と標準品質版を作り、字幕は焼き付けません。
完成ファイルのtitleメタデータをファイル名へ揃えるため、VLCやDLNAクライアントで
連番どおり識別できます。

## 必要環境

- macOS
- Go 1.26系
- Homebrew
- Google ChromeまたはMicrosoft Edge
- HandBrakeCLI 1.11系
- FFmpeg / ffprobe 8系
- MKVToolNix（mkvpropedit）
- 同じ親ディレクトリに`local-web-app-server`リポジトリ

HandBrakeCLI、FFmpeg、ffprobe、mkvpropeditがない場合、既定の`make`が
Homebrewで導入します。汎用Local Web App Serverは動画ツールを認識せず、
このリポジトリだけが依存確認を所有します。

HandBrake GUIからエクスポートした`My Presets.json`を
`~/Documents/ChapterBrake/My Presets.json`へ置くと、その中のプリセットを
My Presets一覧へ表示してHandBrakeCLIへ明示的に読み込ませます。GUIアプリ自体や
GUI内部設定には依存しません。ファイルがない場合だけ従来相当の4件を表示します。

## インストールと起動

通常操作は次だけです。

```sh
make
```

`make`は次を冪等に行います。

1. 必須動画ツールを確認し、不足分だけ導入する。
2. 隣の`local-web-app-server`をビルドして`~/.local/bin`へ配置する。
3. ChapterBrakeバックエンドをビルドする。
4. マニフェスト、Webファイル、バックエンドを登録する。
5. 実行中の汎用サーバーを正常終了し、更新済みアプリで起動し直す。
6. `http://127.0.0.1:8766/apps/chapter-brake/`を既定ブラウザで開く。

サーバーは既定で`0.0.0.0:8766`を待ち受けます。同じ信頼済みLAN上のPCからは
`http://<このMacのLAN IP>:8766/apps/chapter-brake/`へ接続できます。ブラウザは
操作要求を送るだけで、解析、キュー保存、エンコード、後処理はすべてこのMac上で
実行されます。TLSとHTTP認証はないため、インターネット公開やルーターのポート転送は
行わないでください。ループバック限定に戻す場合は`make SERVER_LISTEN=127.0.0.1:8766`
を使用します。

実行中ジョブがある場合、サーバー更新はそのジョブの完了を待ち、残りのキューを
一時停止してから行います。即時中断にはなりません。二回目以降の`make`も同じ
登録先を置き換えるため、アプリやプロセスを重複作成しません。

開発用ターゲット:

```sh
make build
make test
make check
make doctor
make run
make open
make stop
```

`chapterbrake`バイナリはLocal Web App Serverが
`LOCAL_WEB_SOCKET`を付けて起動するバックエンドです。直接起動はしません。
一時的な入力開始ディレクトリを指定する`--directory PATH` / `-d PATH`は、
独自のインストールマニフェストでバックエンド引数を指定する場合に利用できます。

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

~/Library/Application Support/LocalWebAppServer/apps/chapter-brake/
├── local-web-app.json
├── web/
└── bin/chapterbrake
```

初期入力先は`/Volumes/2TB HDD/Images`、初期出力先は
`/Volumes/2TB HDD/Movies`、初期区切り時間は`23:40`です。右上の設定ボタンから
3項目を変更でき、`settings.json`へ原子的に保存します。入力先は存在する
ディレクトリ、出力先は絶対パスを指定します。出力先自体は未作成でも保存できます。

入力先はバックエンド起動時に存在するディレクトリである必要があります。
出力先とタイトル名の子ディレクトリはジョブ開始時に必要なら作成します。
実行中に出力先を削除した場合や、作成・書き込みに失敗した場合は、そのジョブを
失敗として先頭に保持します。

## 操作の流れ

1. 起動直後の入力画面、または「新しいジョブ」を押す。
2. サーバー側のディレクトリを移動し、MKVを一つ選ぶ。
3. My PresetsまたはHandBrake標準プリセットを選ぶ。
4. 出力ベース名と開始番号を確認する。
5. 区切り時間、末尾除外、出力開始チャプターを選ぶ。
6. 入力音声を選ぶ。
7. MKVの場合だけソフト字幕を選ぶ。
8. 出力名、チャプター範囲、音声、字幕をプレビューする。
9. 既存出力があれば上書きを明示承認する。
10. キューへ追加する。

追加後は同じ入力ディレクトリへ戻るため、エンコードを進めながら続けて登録できます。
右ペインで進捗、段階、参考動画時間、全体ETAを確認できます。ジョブを押すと
詳細モーダルを表示します。
実行中ジョブはHandBrake段階で一時停止・再開でき、現在ジョブ後停止と
即時中断も選べます。待機ジョブは右ペインから確認付きで削除でき、ドラッグ＆ドロップ
で順序を変更できます。

ドラッグ＆ドロップにはChapterBrakeへ同梱したSortableJS 1.15.7を使用します。
汎用サーバーは各WebアプリのUIライブラリを管理せず、アプリごとにバージョンを
固定します。ライセンスは`web/vendor/SORTABLE_LICENSE.txt`に収録しています。

ブラウザの再読み込みや終了はエンコードを停止しません。再度URLを開くと、
永続キューと現在状態を復元します。

## チャプター、音声、字幕

切り出し範囲にはHandBrakeCLIの`--chapters N-M`だけを使います。秒、フレーム、
PTS指定やエンコード後の境界補正は行いません。HandBrake GUIでも発生する
素材依存の境界挙動はHandBrakeの仕様として扱います。

チャプター画面には動画全体、各チャプターの開始、単体時間、選択位置からの
出力合計時間を表示します。自動近似で短い末尾単体出力が生じる場合は直前区間へ
結合し、2秒以下の最終チャプターは初期状態で除外します。どちらも画面で確認・変更
できます。

音声ビットレートの数値入力はありません。選択した各入力トラックから次を作ります。

- 高品質: AC-3パススルー、非対応時は高品質AAC。
- 標準品質: AAC stereo。

MKVでは選択字幕をソフト字幕として格納できます。MP4は字幕なしです。
どちらもHandBrakeの自動字幕選択と焼き付けを明示的に無効化します。

## キュー、上書き、中断

`queue.json`の配列順に一件ずつ実行します。先頭ジョブは、エンコード、title設定、
ffprobe検証、最終rename、queue保存が成功するまで残ります。失敗時は後続へ進みません。

承認済み既存出力はジョブ開始時に再確認せず置き換えます。HandBrakeCLIは
`<出力先>/<ベース名>/`のジョブ固有一時ファイルへ書き、検証後だけ最終名へ
renameします。

「即時中断して一時停止」は外部プロセスの終了を待って部分出力を削除し、
キュー先頭を残して後続を止めます。次の開始では同じジョブを最初から実行します。
汎用サーバーやバックエンドの通常終了は現在ジョブを完了し、残りを一時停止して
から終了します。

## title後処理

- MKV: `mkvpropedit`で一時MKVのsegment titleだけを直接変更する。
- MP4: `ffmpeg -c copy`で再エンコードせず別の一時MP4へtitleを設定する。
- `ffprobe`でtitle、ストリーム、チャプター、時刻構造を検証後だけ公開する。

title値は最終ファイル名から拡張子を除いた文字列です。Twonkyへ置く前の確認は、
完成ファイルをVLCで直接開けば十分です。

## ログ

`~/Library/Logs/ChapterBrake/`へ日別アプリログ、ジョブログ、HandBrakeCLI、
ffmpeg、ffprobe、mkvpropeditのstdout/stderr生ログを保存します。Web UIのログ表示は
この正本ログの差分表示です。自動削除、圧縮、ローテーションは行いません。

## 開発時の検証

```sh
make check
```

実ツール統合試験:

```sh
CHAPTERBRAKE_INTEGRATION=1 \
CHAPTERBRAKE_FIXTURE=/absolute/path/to/source-four-chapters.mkv \
go test ./internal/runner -run TestRealToolchainIntegration -v -count=1
```

詳細は`docs/WEB_API.md`、`docs/WEB_UI.md`、`docs/ACCEPTANCE_TESTS.md`を参照してください。

## ライセンス

MIT Licenseです。詳細は[LICENSE](LICENSE)を参照してください。
