package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"chapterbrake/internal/app"
	"chapterbrake/internal/handbrake"
	"chapterbrake/internal/media"
	"chapterbrake/internal/queue"
	"chapterbrake/internal/runner"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type UI struct {
	terminal   *tview.Application
	pages      *tview.Pages
	service    *app.Service
	runner     *runner.Runner
	initialDir string

	draft   app.Draft
	preview app.Preview
	adding  bool
	addID   uint64

	mu              sync.Mutex
	running         bool
	cancel          context.CancelFunc
	stopAfterCancel bool
}

func New(service *app.Service, queueRunner *runner.Runner, initialDirectory string) (*UI, error) {
	if service == nil {
		return nil, fmt.Errorf("app service is nil")
	}
	if service.Presets == nil {
		return nil, fmt.Errorf("preset catalog is nil")
	}
	if queueRunner == nil {
		return nil, fmt.Errorf("queue runner is nil")
	}
	if !filepath.IsAbs(initialDirectory) {
		return nil, fmt.Errorf("initial directory must be absolute: %q", initialDirectory)
	}
	ui := &UI{
		terminal:   tview.NewApplication(),
		pages:      tview.NewPages(),
		service:    service,
		runner:     queueRunner,
		initialDir: initialDirectory,
	}
	ui.showMain()
	ui.terminal.SetRoot(ui.pages, true)
	ui.terminal.SetInputCapture(ui.captureGlobalInput)
	return ui, nil
}

func (u *UI) Run() error {
	return u.terminal.Run()
}

func (u *UI) Stop() {
	u.terminal.Stop()
}

func (u *UI) Shutdown() {
	u.mu.Lock()
	if u.running && u.cancel != nil {
		u.stopAfterCancel = true
		u.cancel()
		u.mu.Unlock()
		return
	}
	u.mu.Unlock()
	u.terminal.Stop()
}

func (u *UI) Application() *tview.Application {
	return u.terminal
}

func (u *UI) Pages() *tview.Pages {
	return u.pages
}

func (u *UI) showMain() {
	u.adding = false
	q, err := u.service.Queue()
	waiting := 0
	if err == nil {
		waiting = len(q.Jobs)
	}
	list := tview.NewList().
		AddItem("新しいジョブを追加", "", 'a', u.startAdd).
		AddItem("キューを表示", "", 'q', u.showQueue).
		AddItem("キューを実行", "", 'r', u.startQueue).
		AddItem("終了", "", 'x', u.terminal.Stop)
	list.SetTitle(fmt.Sprintf(" ChapterBrake — 待機中: %d件 — ↑↓:移動 →/Enter:決定 ", waiting)).SetBorder(true)
	list.SetInputCapture(listNavigation(nil, nil))
	u.switchPage("main", list)
}

func (u *UI) startAdd() {
	u.addID++
	u.adding = true
	u.showFiles(u.initialDir)
}

func (u *UI) showFiles(directory string) {
	entries, err := app.ListInputEntries(directory)
	if err != nil {
		u.showError("ファイル一覧", err, u.showMain)
		return
	}
	list := tview.NewList()
	list.SetTitle(" 入力MKVを選択 — ↑↓:移動 →/Enter:決定 ←:親 BS/Esc:戻る — " + directory + " ").SetBorder(true)
	for _, entry := range entries {
		entry := entry
		secondary := ""
		if !entry.IsDir {
			secondary = humanSize(entry.Size)
		}
		list.AddItem(entry.Name, secondary, 0, func() {
			if entry.IsDir {
				u.showFiles(entry.Path)
				return
			}
			u.analyze(entry.Path)
		})
	}
	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEsc, tcell.KeyBackspace, tcell.KeyBackspace2:
			u.showMain()
			return nil
		case tcell.KeyLeft:
			parent := filepath.Dir(directory)
			if parent != directory {
				u.showFiles(parent)
			}
			return nil
		case tcell.KeyRight:
			return enterKey()
		}
		return event
	})
	u.switchPage("files", list)
}

func (u *UI) analyze(input string) {
	addID := u.addID
	u.showBusy("入力を解析中…")
	go func() {
		draft, err := u.service.Analyze(context.Background(), input)
		u.terminal.QueueUpdateDraw(func() {
			if !u.addActive(addID) {
				return
			}
			if err != nil {
				u.showError("入力解析", err, func() { u.showFiles(filepath.Dir(input)) })
				return
			}
			u.draft = draft
			u.showPreset()
		})
	}()
}

func (u *UI) showPreset() {
	list := tview.NewList()
	list.SetTitle(" プリセット — My Presets相当 — ↑↓:移動 →/Enter:決定 ←/BS:戻る Esc:トップ ").SetBorder(true)
	for _, preset := range u.service.Presets.Curated() {
		preset := preset
		list.AddItem(preset.DisplayName, preset.Summary, 0, func() {
			u.selectPreset(preset)
		})
	}
	list.AddItem("その他のプリセットから選ぶ", "HandBrake標準プリセット", 0, u.loadStandardPresets)
	back := func() { u.showFiles(filepath.Dir(u.draft.Input)) }
	list.SetInputCapture(listNavigation(back, u.showMain))
	u.switchPage("preset", list)
}

func (u *UI) loadStandardPresets() {
	addID := u.addID
	u.showBusy("HandBrake標準プリセットを取得中…")
	go func() {
		presets, err := u.service.Presets.ListStandard(context.Background(), nil, nil)
		u.terminal.QueueUpdateDraw(func() {
			if !u.addActive(addID) {
				return
			}
			if err != nil {
				u.showError("プリセット一覧", err, u.showPreset)
				return
			}
			u.showStandardPresets(presets)
		})
	}()
}

func (u *UI) showStandardPresets(presets []handbrake.StandardPreset) {
	list := tview.NewList()
	list.SetTitle(" その他のプリセット — ↑↓:移動 →/Enter:決定 ←/BS:戻る Esc:トップ ").SetBorder(true)
	for _, preset := range presets {
		preset := preset
		list.AddItem(preset.Name, preset.Category, 0, func() {
			u.resolveStandardPreset(preset.Name)
		})
	}
	list.SetInputCapture(listNavigation(u.showPreset, u.showMain))
	u.switchPage("standard-presets", list)
}

func (u *UI) resolveStandardPreset(name string) {
	addID := u.addID
	u.showBusy("プリセットを確認中…")
	go func() {
		preset, err := u.service.Presets.Resolve(context.Background(), name, nil, nil)
		u.terminal.QueueUpdateDraw(func() {
			if !u.addActive(addID) {
				return
			}
			if err != nil {
				u.showError("プリセット", err, u.showPreset)
				return
			}
			u.selectPreset(preset)
		})
	}()
}

func (u *UI) selectPreset(preset handbrake.Preset) {
	u.draft.Preset = preset
	if err := u.service.InitializeNaming(&u.draft); err != nil {
		u.showError("出力名初期化", err, u.showPreset)
		return
	}
	u.showNaming()
}

func (u *UI) showNaming() {
	base := tview.NewInputField().SetLabel("出力ベース名: ").SetText(u.draft.Base).SetFieldWidth(48)
	start := tview.NewInputField().
		SetLabel("開始番号: ").
		SetText(strconv.Itoa(u.draft.StartIndex)).
		SetAcceptanceFunc(tview.InputFieldInteger).
		SetFieldWidth(8)
	next := func() {
		value, err := strconv.Atoi(start.GetText())
		if err != nil || value < 1 {
			u.showError("出力名", fmt.Errorf("開始番号は1以上にしてください"), u.showNaming)
			return
		}
		if strings.TrimSpace(base.GetText()) == "" {
			u.showError("出力名", fmt.Errorf("出力ベース名は空にできません"), u.showNaming)
			return
		}
		u.draft.Base = base.GetText()
		u.draft.StartIndex = value
		u.showChapters()
	}
	form := tview.NewForm().
		AddFormItem(base).
		AddFormItem(start).
		AddTextView("出力先: ", u.service.OutputDirectory, 70, 2, false, false).
		AddButton("次へ", next).
		AddButton("戻る", u.showPreset)
	form.SetTitle(" 出力名 — Enter:次 ↑↓/Tab:移動 ←→:カーソル BS:削除 Esc:トップ ").SetBorder(true)
	form.SetInputCapture(formNavigation(form, u.showPreset, u.showMain, next))
	u.switchPage("naming", form)
}

func (u *UI) showChapters() {
	intervalLabel := media.FormatChapterInterval(u.draft.ChapterInterval)
	selected := intSet(u.draft.SelectedChapters)
	form := tview.NewForm()
	interval := tview.NewInputField().
		SetLabel("区切り時間 (分:秒 / Enterで再計算): ").
		SetText(intervalLabel).
		SetFieldWidth(8)
	form.AddFormItem(interval)
	boxes := make([]*tview.Checkbox, len(u.draft.Media.Chapters))
	refresh := func() {}
	for i, chapter := range u.draft.Media.Chapters {
		chapter := chapter
		box := tview.NewCheckbox().SetChecked(selected[chapter.Number])
		box.SetChangedFunc(func(checked bool) {
			u.draft.AutoChapters = false
			if checked {
				selected[chapter.Number] = true
			} else {
				delete(selected, chapter.Number)
			}
			u.draft.SelectedChapters = sortedKeys(selected)
			refresh()
		})
		boxes[i] = box
		form.AddFormItem(box)
	}
	refresh = func() {
		mode := "手動"
		if u.draft.AutoChapters {
			mode = intervalLabel + "近似: 有効"
		}
		form.SetTitle(" チャプター開始位置 — " + mode + " — ↑↓:移動 ←→/Space:切替 Enter:次 Esc:トップ ")
		elapsed, available := elapsedForDisplay(u.draft.Media.Chapters, sortedKeys(selected))
		for i, chapter := range u.draft.Media.Chapters {
			relative := "-"
			if available[i] {
				relative = formatDuration(elapsed[i])
			}
			boxes[i].SetLabel(fmt.Sprintf(
				"%03d  %s  %s  %s",
				chapter.Number,
				formatDuration(chapter.Start),
				relative,
				chapter.Title,
			))
		}
	}
	refresh()
	updateApproximation := func(force bool) error {
		chapterInterval, err := media.ParseChapterInterval(interval.GetText())
		if err != nil {
			return fmt.Errorf("区切り時間は分:秒で入力してください: %w", err)
		}
		if !force && chapterInterval == u.draft.ChapterInterval {
			return nil
		}
		starts, err := media.ApproximateStarts(u.draft.Media.Chapters, chapterInterval)
		if err != nil {
			return err
		}
		u.draft.ChapterInterval = chapterInterval
		u.draft.SelectedChapters = starts
		u.draft.AutoChapters = true
		return nil
	}
	next := func() {
		if err := updateApproximation(false); err != nil {
			u.showError("チャプター", err, u.showChapters)
			return
		}
		if len(u.draft.SelectedChapters) == 0 {
			u.showError("チャプター", fmt.Errorf("少なくとも1件を選択してください"), u.showChapters)
			return
		}
		u.showAudio()
	}
	form.
		AddButton("入力した時間の近似値にチェック", func() {
			if err := updateApproximation(true); err != nil {
				u.showError("チャプター", err, u.showChapters)
				return
			}
			u.showChapters()
		}).
		AddButton("すべてのチェックを外す", func() {
			u.draft.SelectedChapters = []int{}
			u.draft.AutoChapters = false
			u.showChapters()
		}).
		AddButton("次へ", next).
		AddButton("戻る", u.showNaming)
	form.SetBorder(true)
	navigate := formNavigation(form, u.showNaming, u.showMain, next)
	form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		formItem, _ := form.GetFocusedItemIndex()
		if formItem == 0 && event.Key() == tcell.KeyEnter {
			if err := updateApproximation(true); err != nil {
				u.showError("チャプター", err, u.showChapters)
				return nil
			}
			u.showChapters()
			return nil
		}
		return navigate(event)
	})
	u.switchPage("chapters", form)
}

func (u *UI) showAudio() {
	selected := intSet(u.draft.AudioTracks)
	form := tview.NewForm()
	for _, track := range u.draft.Media.AudioTracks {
		track := track
		label := fmt.Sprintf("%d  %s  %s  %s", track.Number, track.Language, track.Codec, channelLabel(track.Channels))
		if track.Number > 2 {
			form.AddTextView("", "[-] "+label+"  初期版では未対応", 70, 1, false, false)
			continue
		}
		form.AddCheckbox(label, selected[track.Number], func(checked bool) {
			if checked {
				selected[track.Number] = true
			} else {
				delete(selected, track.Number)
			}
			u.draft.AudioTracks = sortedKeys(selected)
		})
	}
	form.AddTextView("", "各選択トラックから: 高品質 + 標準品質（数値入力なし）", 70, 2, false, false)
	next := func() {
		if len(u.draft.AudioTracks) == 0 {
			u.showError("音声", fmt.Errorf("少なくとも1トラックを選択してください"), u.showAudio)
			return
		}
		if u.draft.Preset.Container == queue.ContainerMP4 {
			u.draft.Subtitles = []int{}
			u.showPreview()
			return
		}
		u.showSubtitles()
	}
	form.AddButton("次へ", next)
	form.AddButton("戻る", u.showChapters)
	form.SetTitle(" 音声トラック — ↑↓:移動 ←→/Space:切替 Enter:次 Esc:トップ ").SetBorder(true)
	form.SetInputCapture(formNavigation(form, u.showChapters, u.showMain, next))
	u.switchPage("audio", form)
}

func (u *UI) showSubtitles() {
	selected := intSet(u.draft.Subtitles)
	form := tview.NewForm()
	updating := false
	var none *tview.Checkbox
	var boxes []*tview.Checkbox
	none = tview.NewCheckbox().SetLabel("字幕を入れない").SetChecked(len(selected) == 0)
	none.SetChangedFunc(func(checked bool) {
		if updating || !checked {
			return
		}
		updating = true
		selected = map[int]bool{}
		u.draft.Subtitles = []int{}
		for _, box := range boxes {
			box.SetChecked(false)
		}
		updating = false
	})
	form.AddFormItem(none)
	for _, track := range u.draft.Media.SubtitleTracks {
		track := track
		box := tview.NewCheckbox().
			SetLabel(fmt.Sprintf("%d  %s  %s  %s", track.Number, track.Language, track.Format, track.Name)).
			SetChecked(selected[track.Number])
		box.SetChangedFunc(func(checked bool) {
			if updating {
				return
			}
			updating = true
			if checked {
				selected[track.Number] = true
				none.SetChecked(false)
			} else {
				delete(selected, track.Number)
				if len(selected) == 0 {
					none.SetChecked(true)
				}
			}
			u.draft.Subtitles = sortedKeys(selected)
			updating = false
		})
		boxes = append(boxes, box)
		form.AddFormItem(box)
	}
	form.AddTextView("", "焼き付け: 無効", 40, 1, false, false)
	next := u.showPreview
	form.AddButton("次へ", next)
	form.AddButton("戻る", u.showAudio)
	form.SetTitle(" 字幕 — ↑↓:移動 ←→/Space:切替 Enter:次 Esc:トップ ").SetBorder(true)
	form.SetInputCapture(formNavigation(form, u.showAudio, u.showMain, next))
	u.switchPage("subtitles", form)
}

func (u *UI) showPreview() {
	preview, err := u.service.BuildPreview(u.draft)
	if err != nil {
		u.showError("プレビュー", err, u.showAudio)
		return
	}
	u.preview = preview
	var text strings.Builder
	fmt.Fprintf(&text, "入力: %s\n", u.draft.Input)
	fmt.Fprintf(&text, "プリセット: %s (%s)\n", u.draft.Preset.DisplayName, u.draft.Preset.Container)
	fmt.Fprintf(&text, "出力先: %s\n出力数: %d\n\n", u.service.OutputDirectory, len(preview.Jobs))
	for _, job := range preview.Jobs {
		duration, _ := (media.ChapterRange{
			Start: job.ChapterStart,
			End:   job.ChapterEnd,
		}).ApproximateDuration(u.draft.Media.Chapters, u.draft.Media.Duration)
		fmt.Fprintf(&text, "%s  chapter %d-%d  約%s  title=%s\n",
			filepath.Base(job.Output),
			job.ChapterStart,
			job.ChapterEnd,
			formatDuration(duration),
			strings.TrimSuffix(filepath.Base(job.Output), filepath.Ext(job.Output)),
		)
	}
	if preview.Excluded != nil {
		fmt.Fprintf(&text, "\n除外: chapter %d-%d\n", preview.Excluded.Start, preview.Excluded.End)
	}
	fmt.Fprintf(&text, "\n入力音声: %v\n各入力から: 高品質 + 標準品質\n", u.draft.AudioTracks)
	fmt.Fprintf(&text, "字幕: %v\n焼き付け: 無効\n", u.draft.Subtitles)
	if len(preview.Collisions) > 0 {
		text.WriteString("\n[red]既存出力（追加には上書き承認が必要）:[white]\n")
		for _, path := range preview.Collisions {
			fmt.Fprintf(&text, "  %s\n", path)
		}
	}

	body := tview.NewTextView().SetDynamicColors(true).SetText(text.String())
	form := tview.NewForm()
	label := "キューへ追加"
	if len(preview.Collisions) > 0 {
		label = "上書きを承認してキューへ追加"
	}
	form.AddButton(label, func() {
		if err := u.service.AddPreview(u.preview, len(u.preview.Collisions) > 0); err != nil {
			u.showError("キュー追加", err, u.showPreview)
			return
		}
		u.showMessage("キュー追加", fmt.Sprintf("%d件を追加しました", len(u.preview.Jobs)), u.showMain)
	})
	back := func() {
		if u.draft.Preset.Container == queue.ContainerMKV {
			u.showSubtitles()
		} else {
			u.showAudio()
		}
	}
	form.AddButton("戻る", back)
	form.SetInputCapture(formNavigation(form, back, u.showMain, nil))
	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(body, 0, 1, false).
		AddItem(form, 3, 0, true)
	layout.SetTitle(" 実行プレビュー — ←→/↑↓:移動 BS/Esc:戻る ").SetBorder(true)
	u.switchPage("preview", layout)
}

func (u *UI) showQueue() {
	q, err := u.service.Queue()
	if err != nil {
		u.showError("キュー", err, u.showMain)
		return
	}
	list := tview.NewList()
	for i, job := range q.Jobs {
		list.AddItem(
			fmt.Sprintf("%d. %s", i+1, filepath.Base(job.Output)),
			fmt.Sprintf("chapter %d-%d", job.ChapterStart, job.ChapterEnd),
			0,
			nil,
		)
	}
	if len(q.Jobs) == 0 {
		list.AddItem("キューは空です", "", 0, nil)
	}
	list.SetTitle(" キュー（表示のみ）— ←/BS/Esc:戻る ").SetBorder(true)
	list.SetInputCapture(listNavigation(u.showMain, u.showMain))
	u.switchPage("queue", list)
}

func (u *UI) startQueue() {
	q, err := u.service.Queue()
	if err != nil {
		u.showError("キュー実行", err, u.showMain)
		return
	}
	if len(q.Jobs) == 0 {
		u.showMessage("キュー実行", "キューは空です", u.showMain)
		return
	}

	u.mu.Lock()
	if u.running {
		u.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	u.running = true
	u.cancel = cancel
	u.mu.Unlock()

	status := tview.NewTextView().SetDynamicColors(true)
	status.SetTitle(" キュー実行 — Ctrl+C: 即時中断 ").SetBorder(true)
	currentJob := q.Jobs[0].ID
	currentStage := "準備中"
	fraction := 0.0
	eta := time.Duration(0)
	jobPositions := make(map[string]int, len(q.Jobs))
	jobOutputs := make(map[string]string, len(q.Jobs))
	for i, job := range q.Jobs {
		jobPositions[job.ID] = i + 1
		jobOutputs[job.ID] = filepath.Base(job.Output)
	}
	render := func() {
		status.SetText(fmt.Sprintf(
			"実行中 %d / %d\n%s\n段階: %s\n進捗: %.1f%%  ETA %s",
			jobPositions[currentJob],
			len(q.Jobs),
			jobOutputs[currentJob],
			currentStage,
			fraction*100,
			formatDuration(eta),
		))
	}
	render()
	u.runner.Stage = func(jobID, stage string) {
		u.terminal.QueueUpdateDraw(func() {
			currentJob = jobID
			currentStage = stage
			if stage != "handbrake" {
				fraction = 0
				eta = 0
			}
			render()
		})
	}
	u.runner.Progress = func(jobID string, progress handbrake.Progress) {
		u.terminal.QueueUpdateDraw(func() {
			currentJob = jobID
			currentStage = "handbrake"
			fraction = progress.Fraction
			eta = time.Duration(progress.ETASeconds) * time.Second
			render()
		})
	}
	u.switchPage("run", status)

	go func() {
		result, runErr := u.runner.Run(ctx)
		u.terminal.QueueUpdateDraw(func() {
			u.mu.Lock()
			u.running = false
			u.cancel = nil
			stop := u.stopAfterCancel
			u.stopAfterCancel = false
			u.mu.Unlock()
			if stop {
				u.terminal.Stop()
				return
			}
			if runErr != nil {
				u.showError("キュー実行", runErr, u.showMain)
				return
			}
			u.showMessage("キュー実行", fmt.Sprintf("%d件完了しました", result.Completed), u.showMain)
		})
	}()
}

func (u *UI) captureGlobalInput(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEsc && u.adding {
		u.showMain()
		return nil
	}
	if event.Key() != tcell.KeyCtrlC {
		return event
	}
	u.mu.Lock()
	cancel := u.cancel
	running := u.running
	u.mu.Unlock()
	if running && cancel != nil {
		cancel()
		return nil
	}
	u.terminal.Stop()
	return nil
}

func (u *UI) addActive(addID uint64) bool {
	return u.adding && u.addID == addID
}

func (u *UI) showBusy(message string) {
	view := tview.NewTextView().SetTextAlign(tview.AlignCenter).SetText("\n\n" + message)
	view.SetBorder(true)
	u.switchPage("busy", view)
}

func (u *UI) showError(title string, err error, done func()) {
	u.showMessage(title, err.Error(), done)
}

func (u *UI) showMessage(title, message string, done func()) {
	close := func() {
		if done != nil {
			done()
		}
	}
	modal := tview.NewModal().
		SetText(message).
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(int, string) {
			close()
		})
	modal.SetTitle(" " + title + " ")
	modal.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEsc, tcell.KeyBackspace, tcell.KeyBackspace2:
			close()
			return nil
		}
		return event
	})
	u.switchPage("message", modal)
}

func (u *UI) switchPage(name string, primitive tview.Primitive) {
	u.pages.RemovePage(name)
	u.pages.AddAndSwitchToPage(name, primitive, true)
	u.terminal.SetFocus(primitive)
}

func listNavigation(back, escape func()) func(*tcell.EventKey) *tcell.EventKey {
	return func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyRight:
			return enterKey()
		case tcell.KeyLeft, tcell.KeyBackspace, tcell.KeyBackspace2:
			if back != nil {
				back()
				return nil
			}
		case tcell.KeyEsc:
			if escape != nil {
				escape()
				return nil
			}
		}
		return event
	}
}

func formNavigation(
	form *tview.Form,
	back func(),
	escape func(),
	next func(),
) func(*tcell.EventKey) *tcell.EventKey {
	return func(event *tcell.EventKey) *tcell.EventKey {
		itemIndex, _ := form.GetFocusedItemIndex()
		var item tview.FormItem
		if itemIndex >= 0 {
			item = form.GetFormItem(itemIndex)
		}
		_, isInput := item.(*tview.InputField)
		_, isCheckbox := item.(*tview.Checkbox)

		if event.Key() == tcell.KeyEsc {
			escape()
			return nil
		}
		if item != nil && event.Key() == tcell.KeyEnter {
			if next != nil {
				next()
			}
			return nil
		}
		if isInput {
			switch event.Key() {
			case tcell.KeyUp:
				return tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone)
			case tcell.KeyDown:
				return tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
			}
			return event
		}
		switch event.Key() {
		case tcell.KeyBackspace, tcell.KeyBackspace2:
			back()
			return nil
		case tcell.KeyUp:
			return tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone)
		case tcell.KeyDown:
			return tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
		case tcell.KeyLeft, tcell.KeyRight:
			if isCheckbox {
				return tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone)
			}
			if event.Key() == tcell.KeyLeft {
				return tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone)
			}
			return tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
		}
		return event
	}
}

func enterKey() *tcell.EventKey {
	return tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)
}

func intSet(values []int) map[int]bool {
	result := make(map[int]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func sortedKeys(values map[int]bool) []int {
	result := make([]int, 0, len(values))
	for value, selected := range values {
		if selected {
			result = append(result, value)
		}
	}
	for i := 1; i < len(result); i++ {
		for j := i; j > 0 && result[j] < result[j-1]; j-- {
			result[j], result[j-1] = result[j-1], result[j]
		}
	}
	return result
}

func elapsedForDisplay(chapters []media.Chapter, selected []int) ([]time.Duration, []bool) {
	if len(selected) == 0 {
		return make([]time.Duration, len(chapters)), make([]bool, len(chapters))
	}
	elapsed, available, err := media.ElapsedFromPreviousSelection(chapters, selected)
	if err != nil {
		return make([]time.Duration, len(chapters)), make([]bool, len(chapters))
	}
	return elapsed, available
}

func formatDuration(duration time.Duration) string {
	if duration < 0 {
		return "-"
	}
	total := int(duration.Round(time.Second) / time.Second)
	hours := total / 3600
	minutes := total % 3600 / 60
	seconds := total % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}

func humanSize(size int64) string {
	const (
		kiB = 1024
		miB = 1024 * kiB
		giB = 1024 * miB
	)
	switch {
	case size >= giB:
		return fmt.Sprintf("%.1f GiB", float64(size)/giB)
	case size >= miB:
		return fmt.Sprintf("%.1f MiB", float64(size)/miB)
	case size >= kiB:
		return fmt.Sprintf("%.1f KiB", float64(size)/kiB)
	default:
		return fmt.Sprintf("%d B", size)
	}
}

func channelLabel(channels int) string {
	switch channels {
	case 1:
		return "Mono"
	case 2:
		return "Stereo"
	case 6:
		return "5.1ch"
	default:
		return fmt.Sprintf("%dch", channels)
	}
}
