# ChapterBrake — 設計パッケージ

macOS上でMKVソースを選択し、HandBrakeCLIのプリセット、チャプター分割、音声、字幕を対話的に設定して、永続キューへ追加・順次実行するGoアプリケーション「ChapterBrake」の設計一式です。出力後はDLNA表示用のタイトルメタデータを出力ファイル名へ統一します。

このZIPには初期実装を含めていません。Codexは最初に短い検証用MKVを用いた実エンコードPoCを行い、チャプター、音声、字幕非焼き付け、プリセット、MKV/MP4のタイトル設定、DLNA表示、即時中断が成立することを証明します。`docs/POC_RESULT.md`が`GO`になるまで製品実装は禁止です。

## Codexでの開始方法

1. このディレクトリをCodexのプロジェクトディレクトリとして開く。
2. `CODEX_START.md`の内容を最初の指示として渡す。
3. CodexにMilestone 0のPoCだけを実行させ、GO/NO-GO報告で一度停止させる。
4. `docs/POC_RESULT.md`が`GO`になった後に、`prompts/02-core.md`以降を順に渡す。

詳細仕様は`docs/`、実行計画は`PLANS.md`にあります。

## 実装前PoC

PoCの正本は`docs/POC_GATE.md`です。ヘルプ確認だけでは完了せず、実際の短いエンコードとffprobe・映像/音声ハッシュ・mkvpropedit・ffmpeg・DLNA表示・プロセス停止試験を必須とします。PoC用スパイクは`poc/`へ隔離します。

## 想定成果物

実装後の概略構成は次を想定しています。Codexの調査結果により小規模な変更は可能ですが、不要な階層追加は禁止です。

```text
.
├── cmd/
│   └── chapterbrake/
│       └── main.go
├── internal/
│   ├── app/
│   ├── config/
│   ├── handbrake/
│   ├── metadata/
│   ├── media/
│   ├── queue/
│   ├── runner/
│   ├── logging/
│   └── tui/
├── docs/
├── AGENTS.md
├── PLANS.md
├── go.mod
└── README.md
```

## ランタイムデータ

```text
~/Documents/ChapterBrake/
├── settings.json
├── queue.json
└── logs/
```

初期設定の出力先は次です。

```text
/Volumes/2TB HDD/mp4/
```
