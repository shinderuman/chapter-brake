package tui

import (
	"context"
	"errors"
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
	"chapterbrake/internal/runstate"

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

	mu                sync.Mutex
	running           bool
	cancel            context.CancelFunc
	exitAfterCurrent  bool
	queuePaused       bool
	pauseAfterCurrent bool
	runSummary        string
	currentJobID      string
	currentStage      string
	currentProgress   float64
	currentETA        time.Duration
	currentDuration   time.Duration
	currentStartedAt  time.Time
	currentPausedAt   time.Time
	currentPausedFor  time.Duration
	encodingPaused    bool
	speedFactor       float64
	mainSelection     int
	queueSelection    int
	lastRunError      string
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
	if u.running {
		u.pauseAfterCurrent = true
		u.exitAfterCurrent = true
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
	waiting := len(q.Jobs)
	u.mu.Lock()
	running := u.running
	paused := u.queuePaused
	summary := u.runSummary
	snapshot := u.queueSnapshotLocked()
	u.mu.Unlock()
	alert, alertErr := u.queueAlert()
	queueLabel := "キュー・実行状況"
	list := tview.NewList().
		AddItem("新しいジョブを追加", "", 'a', u.startAdd).
		AddItem(queueLabel, "", 'q', u.showQueue).
		AddItem("終了", "", 'x', u.requestExit)
	list.ShowSecondaryText(false)
	list.SetCurrentItem(u.mainSelection)
	list.SetChangedFunc(func(index int, _, _ string, _ rune) {
		u.mainSelection = index
	})
	state := fmt.Sprintf("待機中: %d件", waiting)
	if running {
		state = fmt.Sprintf("キュー実行中: %d件", waiting)
	} else if paused {
		state = fmt.Sprintf("キュー一時停止中: %d件", waiting)
	} else if summary != "" {
		state += " — " + summary
	}
	list.SetInputCapture(listNavigation(nil, nil))
	queueView := tview.NewTextView().SetDynamicColors(true)
	if err != nil {
		queueView.SetText("[red]キュー読込エラー: " + err.Error() + "[white]")
	} else {
		queueView.SetText(formatQueueOverview(q, snapshot))
	}
	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(list, 5, 0, true).
		AddItem(queueView, 0, 1, false)
	title := fmt.Sprintf(" ChapterBrake — %s — ↑↓:移動 →/Enter:決定 ", state)
	if alertErr != nil {
		title = " ChapterBrake — 状態読込エラー: " + alertErr.Error() + " "
		layout.SetBorderColor(tcell.ColorRed)
	} else if alert.Status == runstate.StatusFailed {
		title = " ChapterBrake — 異常停止: " + alert.Message + " "
		layout.SetBorderColor(tcell.ColorRed)
	}
	layout.SetTitle(title).SetBorder(true)
	u.switchPage("main", layout)
}

func (u *UI) requestExit() {
	u.mu.Lock()
	running := u.running
	u.mu.Unlock()
	if !running {
		u.terminal.Stop()
		return
	}

	modal := tview.NewModal().
		SetText("キューを実行中です。\n現在のジョブを完了してからキューを一時停止し、ChapterBrakeを終了しますか？").
		AddButtons([]string{"現在ジョブ完了後に終了", "戻る"}).
		SetDoneFunc(func(_ int, label string) {
			if label != "現在ジョブ完了後に終了" {
				u.showMain()
				return
			}
			u.mu.Lock()
			u.pauseAfterCurrent = true
			u.exitAfterCurrent = true
			u.mu.Unlock()
			u.showQueue()
		})
	modal.SetTitle(" 終了確認 ")
	modal.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEsc, tcell.KeyBackspace, tcell.KeyBackspace2:
			u.showMain()
			return nil
		}
		return event
	})
	u.switchPage("exit-confirm", modal)
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
	list.SetTitle(" 入力MKVを選択 — ↑↓:移動 →/Enter:決定 BS/Esc:戻る — " + directory + " ").SetBorder(true)
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
		AddTextView("出力先: ", filepath.Join(u.service.OutputDirectory, "<出力ベース名>"), 70, 2, false, false).
		AddButton("次へ", next).
		AddButton("戻る", u.showPreset)
	form.SetTitle(" 出力名 — Enter:次 ↑↓/Tab:移動 ←→:カーソル BS:削除 Esc:トップ ").SetBorder(true)
	form.SetInputCapture(formNavigation(form, u.showPreset, u.showMain, next))
	u.switchPage("naming", form)
}

func (u *UI) showChapters() {
	intervalLabel := media.FormatChapterInterval(u.draft.ChapterInterval)
	selected := intSet(u.draft.SelectedChapters)
	interval := tview.NewInputField().
		SetLabel("区切り時間 (分:秒): ").
		SetText(intervalLabel).
		SetFieldWidth(8)
	intervalForm := tview.NewForm().AddFormItem(interval)
	summary := tview.NewTextView().SetDynamicColors(true)
	table := tview.NewTable().
		SetSelectable(true, false).
		SetFixed(1, 0).
		SetSelectedStyle(tcell.StyleDefault.Background(tcell.ColorBlue).Foreground(tcell.ColorWhite))
	headers := []string{"選択", "番号", "開始", "単体", "出力合計", "タイトル"}
	for column, header := range headers {
		table.SetCell(
			0,
			column,
			tview.NewTableCell(header).
				SetTextColor(tcell.ColorWhite).
				SetSelectable(false).
				SetAttributes(tcell.AttrBold),
		)
	}
	refresh := func() {}
	updatingFinal := false
	var excludeFinal *tview.Checkbox
	var excludeForm *tview.Form
	finalChapter := len(u.draft.Media.Chapters)
	if finalChapter > 1 {
		finalDuration, err := media.FinalChapterDuration(u.draft.Media.Chapters, u.draft.Media.Duration)
		if err != nil {
			u.showError("チャプター", err, u.showNaming)
			return
		}
		excludeFinal = tview.NewCheckbox().
			SetLabel(fmt.Sprintf(
				"末尾の短いチャプターを除外 (chapter %03d / %s / 2秒以下は自動)",
				finalChapter,
				formatDuration(finalDuration),
			)).
			SetChecked(u.draft.ExcludeFinal)
		excludeFinal.SetChangedFunc(func(checked bool) {
			if updatingFinal {
				return
			}
			u.draft.ExcludeFinal = checked
			if checked {
				delete(selected, finalChapter)
				u.draft.SelectedChapters = sortedKeys(selected)
			}
			refresh()
		})
		excludeForm = tview.NewForm().AddFormItem(excludeFinal)
	}
	chapterDurations, err := media.ChapterDurations(u.draft.Media.Chapters, u.draft.Media.Duration)
	if err != nil {
		u.showError("チャプター", err, u.showNaming)
		return
	}
	var layout *tview.Flex
	refresh = func() {
		mode := "手動"
		if u.draft.AutoChapters {
			mode = media.FormatChapterInterval(u.draft.ChapterInterval) + "近似: 有効"
		}
		if layout != nil {
			layout.SetTitle(" チャプター開始位置 — " + mode + " — ↑↓:移動 ←→/Space:切替 Enter:次 Esc:トップ ")
		}
		selectedStarts := sortedKeys(selected)
		effectiveFinal := finalChapter
		if u.draft.ExcludeFinal {
			effectiveFinal--
		}
		outputDurations, outputAvailable := outputDurationsForDisplay(
			u.draft.Media.Chapters,
			u.draft.Media.Duration,
			selectedStarts,
			effectiveFinal,
		)
		summaryText := fmt.Sprintf("[yellow]動画全体: %s[white]", formatDuration(u.draft.Media.Duration))
		if u.draft.AutoChapters && u.draft.TailMerged && len(selectedStarts) > 0 {
			lastStart := selectedStarts[len(selectedStarts)-1]
			if lastStart <= effectiveFinal && outputAvailable[lastStart-1] {
				summaryText += fmt.Sprintf(
					"  [yellow]最終出力: Chapter %03d-%03d / %s（末尾を結合）[white]",
					lastStart,
					effectiveFinal,
					formatDuration(outputDurations[lastStart-1]),
				)
			}
		}
		summary.SetText(summaryText)
		for i, chapter := range u.draft.Media.Chapters {
			outputDuration := "-"
			if outputAvailable[i] {
				outputDuration = formatDuration(outputDurations[i])
			}
			title := chapter.Title
			if chapter.Number == finalChapter && u.draft.ExcludeFinal {
				title = strings.TrimSpace(title + "  [末尾除外]")
			}
			checked := "[ ]"
			if selected[chapter.Number] {
				checked = "[x]"
			}
			values := []string{
				checked,
				fmt.Sprintf("%03d", chapter.Number),
				formatDuration(chapter.Start),
				formatDuration(chapterDurations[i]),
				outputDuration,
				title,
			}
			for column, value := range values {
				cell := tview.NewTableCell(value).SetTextColor(tcell.ColorYellow)
				if column == 5 {
					cell.SetExpansion(1)
				}
				table.SetCell(i+1, column, cell)
			}
		}
	}
	updateApproximation := func(force bool) error {
		chapterInterval, err := media.ParseChapterInterval(interval.GetText())
		if err != nil {
			return fmt.Errorf("区切り時間は分:秒で入力してください: %w", err)
		}
		if !force && chapterInterval == u.draft.ChapterInterval {
			return nil
		}
		approximation, err := media.ApproximateStarts(
			u.draft.Media.Chapters,
			u.draft.Media.Duration,
			chapterInterval,
		)
		if err != nil {
			return err
		}
		starts := approximation.Starts
		if u.draft.ExcludeFinal {
			starts = withoutInt(starts, finalChapter)
		}
		u.draft.ChapterInterval = chapterInterval
		u.draft.SelectedChapters = starts
		u.draft.AutoChapters = true
		u.draft.TailMerged = approximation.TailMerged
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
	toggleChapter := func(row int) {
		index := row - 1
		if index < 0 || index >= len(u.draft.Media.Chapters) {
			return
		}
		chapter := u.draft.Media.Chapters[index]
		checked := !selected[chapter.Number]
		if checked && chapter.Number == finalChapter && u.draft.ExcludeFinal {
			updatingFinal = true
			u.draft.ExcludeFinal = false
			if excludeFinal != nil {
				excludeFinal.SetChecked(false)
			}
			updatingFinal = false
		}
		u.draft.AutoChapters = false
		u.draft.TailMerged = false
		if checked {
			selected[chapter.Number] = true
		} else {
			delete(selected, chapter.Number)
		}
		u.draft.SelectedChapters = sortedKeys(selected)
		refresh()
	}
	footer := tview.NewForm().
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
	layout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(intervalForm, 2, 0, true).
		AddItem(summary, 2, 0, false)
	if excludeForm != nil {
		layout.AddItem(excludeForm, 2, 0, false)
	}
	layout.
		AddItem(table, 0, 1, false).
		AddItem(footer, 3, 0, false).
		SetBorder(true)
	refresh()

	focusTable := func() {
		u.terminal.SetFocus(table)
	}
	focusHeader := func() {
		if excludeForm != nil {
			u.terminal.SetFocus(excludeForm)
			return
		}
		u.terminal.SetFocus(intervalForm)
	}
	intervalForm.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEsc:
			u.showMain()
			return nil
		case tcell.KeyEnter:
			next()
			return nil
		case tcell.KeyDown, tcell.KeyTab:
			if excludeForm != nil {
				u.terminal.SetFocus(excludeForm)
			} else {
				focusTable()
			}
			return nil
		}
		return event
	})
	if excludeForm != nil {
		excludeForm.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			switch event.Key() {
			case tcell.KeyEsc:
				u.showMain()
				return nil
			case tcell.KeyBackspace, tcell.KeyBackspace2:
				u.showNaming()
				return nil
			case tcell.KeyEnter:
				next()
				return nil
			case tcell.KeyUp, tcell.KeyBacktab:
				u.terminal.SetFocus(intervalForm)
				return nil
			case tcell.KeyDown, tcell.KeyTab:
				focusTable()
				return nil
			case tcell.KeyLeft, tcell.KeyRight:
				return tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone)
			}
			return event
		})
	}
	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		row, _ := table.GetSelection()
		switch event.Key() {
		case tcell.KeyEsc:
			u.showMain()
			return nil
		case tcell.KeyBackspace, tcell.KeyBackspace2:
			u.showNaming()
			return nil
		case tcell.KeyEnter:
			next()
			return nil
		case tcell.KeyBacktab:
			focusHeader()
			return nil
		case tcell.KeyTab:
			u.terminal.SetFocus(footer)
			return nil
		case tcell.KeyUp:
			if row <= 1 {
				focusHeader()
				return nil
			}
		case tcell.KeyDown:
			if row >= len(u.draft.Media.Chapters) {
				u.terminal.SetFocus(footer)
				return nil
			}
		case tcell.KeyLeft, tcell.KeyRight:
			toggleChapter(row)
			return nil
		case tcell.KeyRune:
			if event.Rune() == ' ' {
				toggleChapter(row)
				return nil
			}
		}
		return event
	})
	footer.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		item, _ := footer.GetFocusedItemIndex()
		if (event.Key() == tcell.KeyUp || event.Key() == tcell.KeyBacktab) && item == 0 {
			u.terminal.SetFocus(table)
			return nil
		}
		return formNavigation(footer, u.showNaming, u.showMain, nil)(event)
	})
	u.switchPage("chapters", layout)
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
	fmt.Fprintf(&text, "出力先: %s\n出力数: %d\n\n", filepath.Dir(preview.Jobs[0].Output), len(preview.Jobs))
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
	if preview.ExcludedFinal != nil {
		duration, _ := preview.ExcludedFinal.ApproximateDuration(
			u.draft.Media.Chapters,
			u.draft.Media.Duration,
		)
		fmt.Fprintf(
			&text,
			"\n末尾除外: chapter %d（約%s）\n",
			preview.ExcludedFinal.Start,
			formatDuration(duration),
		)
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
		u.startQueueAutomatically(len(u.preview.Jobs))
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
	u.showQueueAt(0)
}

func (u *UI) showQueueAt(selectedIndex int) {
	q, err := u.service.Queue()
	if err != nil {
		u.showError("キュー", err, u.showMain)
		return
	}
	list := tview.NewList()
	u.mu.Lock()
	running := u.running
	paused := u.queuePaused
	pauseRequested := u.pauseAfterCurrent
	runSummary := u.runSummary
	exitAfterCurrent := u.exitAfterCurrent
	currentJobID := u.currentJobID
	currentStage := u.currentStage
	currentProgress := u.currentProgress
	currentETA := u.currentETA
	lastRunError := u.lastRunError
	snapshot := u.queueSnapshotLocked()
	u.mu.Unlock()
	for i, job := range q.Jobs {
		job := job
		prefix := fmt.Sprintf("%d.", i+1)
		detail := fmt.Sprintf("約%s / chapter %d-%d", formatJobDuration(job), job.ChapterStart, job.ChapterEnd)
		if running && job.ID == currentJobID {
			runLabel := "実行中"
			if snapshot.encodingPaused {
				runLabel = "一時停止"
			}
			prefix += fmt.Sprintf(" [%s %.1f%%]", runLabel, currentProgress*100)
			detail = fmt.Sprintf(
				"%s / ETA %s / 約%s / chapter %d-%d",
				currentStage,
				formatDuration(currentETA),
				formatJobDuration(job),
				job.ChapterStart,
				job.ChapterEnd,
			)
		} else if !running && i == 0 && lastRunError != "" {
			detail = "停止: " + lastRunError
		}
		list.AddItem(
			fmt.Sprintf("%s %s", prefix, filepath.Base(job.Output)),
			detail,
			0,
			func() {
				u.showQueueJob(job)
			},
		)
	}
	if len(q.Jobs) == 0 {
		list.AddItem("キューは空です", "", 0, nil)
	}
	if selectedIndex >= len(q.Jobs) {
		selectedIndex = len(q.Jobs) - 1
	}
	if selectedIndex >= 0 {
		list.SetCurrentItem(selectedIndex)
	}
	u.queueSelection = selectedIndex
	state := "待機中"
	if running {
		state = "実行中"
		if snapshot.encodingPaused {
			state = "エンコード一時停止中"
		} else if exitAfterCurrent {
			state = "現在ジョブ完了後に終了"
		} else if pauseRequested {
			state = "現在ジョブ後に一時停止"
		}
	} else if paused {
		state = "一時停止中"
	} else if runSummary != "" {
		state = runSummary
	}
	if eta, ok := estimateQueueETA(q, snapshot); ok {
		state += " / 全体ETA 約" + formatDuration(eta)
	}
	list.SetChangedFunc(func(index int, _, _ string, _ rune) {
		u.queueSelection = index
	})
	alert, alertErr := u.queueAlert()
	title := " キュー・実行状況 — " + state + " — ↑↓:選択 Enter:詳細 j:下へ移動 k:上へ移動 ←/BS/Esc:戻る "
	if alertErr != nil {
		title = " キュー・実行状況 — 状態読込エラー: " + alertErr.Error() + " "
		list.SetBorderColor(tcell.ColorRed)
	} else if alert.Status == runstate.StatusFailed {
		title = " キュー・実行状況 — 異常停止: " + alert.Message + " "
		list.SetBorderColor(tcell.ColorRed)
	}
	list.SetTitle(title).SetBorder(true)
	navigate := listNavigation(u.showMain, u.showMain)
	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyRune && (event.Rune() == 'j' || event.Rune() == 'k') && len(q.Jobs) > 0 {
			index := list.GetCurrentItem()
			if index >= 0 && index < len(q.Jobs) {
				delta := 1
				if event.Rune() == 'k' {
					delta = -1
				}
				if err := u.service.MoveQueuedJob(q.Jobs[index].ID, delta); err != nil {
					u.showError("キュー並び替え", err, func() { u.showQueueAt(index) })
					return nil
				}
				u.showQueueAt(index + delta)
			}
			return nil
		}
		return navigate(event)
	})
	u.switchPage("queue", list)
}

func (u *UI) showQueueJob(job queue.Job) {
	u.mu.Lock()
	running := u.running
	currentJobID := u.currentJobID
	pauseRequested := u.pauseAfterCurrent
	currentStage := u.currentStage
	currentProgress := u.currentProgress
	currentETA := u.currentETA
	encodingPaused := u.encodingPaused
	lastRunError := u.lastRunError
	exitAfterCurrent := u.exitAfterCurrent
	u.mu.Unlock()
	q, err := u.service.Queue()
	if err != nil {
		u.showError("キュー詳細", err, u.showQueue)
		return
	}
	active := running && currentJobID == job.ID
	var text strings.Builder
	if active {
		status := "実行中"
		if encodingPaused {
			status = "エンコード一時停止中"
		}
		fmt.Fprintf(
			&text,
			"[yellow]%s %.1f%% / %s / ETA %s[white]\n\n",
			status,
			currentProgress*100,
			currentStage,
			formatDuration(currentETA),
		)
	} else if head, ok := q.Peek(); ok && head.ID == job.ID {
		if lastRunError != "" {
			fmt.Fprintf(&text, "[red]前回停止: %s[white]\n\n", lastRunError)
		} else if alert, err := u.queueAlert(); err == nil && alert.Status == runstate.StatusFailed {
			fmt.Fprintf(&text, "[red]前回異常停止: %s", alert.Message)
			if alert.LogPath != "" {
				fmt.Fprintf(&text, " / log: %s", alert.LogPath)
			}
			text.WriteString("[white]\n\n")
		}
	}
	fmt.Fprintf(&text, "ID: %s\n", job.ID)
	fmt.Fprintf(&text, "入力: %s\n", job.Input)
	fmt.Fprintf(&text, "出力: %s\n", job.Output)
	fmt.Fprintf(&text, "プリセット: %s\n", job.Preset)
	if job.PresetFile != "" {
		fmt.Fprintf(&text, "プリセットファイル: %s\n", job.PresetFile)
	}
	fmt.Fprintf(&text, "形式: %s\n", job.Container)
	fmt.Fprintf(&text, "チャプター: %d-%d\n", job.ChapterStart, job.ChapterEnd)
	fmt.Fprintf(&text, "動画時間: 約%s\n", formatJobDuration(job))
	fmt.Fprintf(&text, "音声: %v\n", job.AudioTracks)
	fmt.Fprintf(&text, "字幕: %v\n", job.Subtitles)
	fmt.Fprintf(&text, "追加日時: %s\n", job.CreatedAt.Local().Format("2006-01-02 15:04:05"))

	body := tview.NewTextView().SetDynamicColors(true).SetText(text.String())
	form := tview.NewForm()
	if active {
		if currentStage == "handbrake" {
			pauseLabel := "エンコードを一時停止"
			if encodingPaused {
				pauseLabel = "エンコードを再開"
			}
			form.AddButton(pauseLabel, func() {
				var err error
				u.mu.Lock()
				paused := u.encodingPaused
				u.mu.Unlock()
				if paused {
					err = u.runner.ResumeCurrent()
				} else {
					err = u.runner.PauseCurrent()
				}
				if err != nil {
					u.showError("エンコード一時停止", err, func() { u.showQueueJob(job) })
					return
				}
				u.mu.Lock()
				now := time.Now()
				if paused {
					if !u.currentPausedAt.IsZero() {
						u.currentPausedFor += now.Sub(u.currentPausedAt)
					}
					u.currentPausedAt = time.Time{}
					u.encodingPaused = false
				} else {
					u.currentPausedAt = now
					u.encodingPaused = true
				}
				u.mu.Unlock()
				u.showQueueJob(job)
			})
		}
		pauseLabel := "現在ジョブ後に一時停止"
		if exitAfterCurrent {
			pauseLabel = "終了予約を取り消す"
		} else if pauseRequested {
			pauseLabel = "一時停止予約を取り消す"
		}
		form.AddButton(pauseLabel, func() {
			u.mu.Lock()
			if u.exitAfterCurrent {
				u.exitAfterCurrent = false
				u.pauseAfterCurrent = false
			} else {
				u.pauseAfterCurrent = !u.pauseAfterCurrent
			}
			u.mu.Unlock()
			u.showQueueJob(job)
		})
		form.AddButton("即時中断して一時停止", func() {
			u.confirmAbortQueueJob(job)
		})
	} else {
		if !running {
			if head, ok := q.Peek(); ok && head.ID == job.ID {
				runLabel := "キューを実行"
				u.mu.Lock()
				if u.queuePaused {
					runLabel = "キューを再開"
				}
				u.mu.Unlock()
				form.AddButton(runLabel, u.startQueue)
			}
		}
		form.AddButton("削除", func() {
			u.confirmDeleteQueueJob(job)
		})
	}
	form.AddButton("戻る", u.showQueue)
	form.SetInputCapture(formNavigation(form, u.showQueue, u.showMain, nil))
	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(body, 0, 1, false).
		AddItem(form, 3, 0, true)
	layout.SetTitle(" キュー詳細 — Enter:決定 ←/BS:戻る Esc:トップ ").SetBorder(true)
	u.switchPage("queue-detail", layout)
}

func (u *UI) confirmAbortQueueJob(job queue.Job) {
	modal := tview.NewModal().
		SetText(fmt.Sprintf(
			"実行中ジョブを即時中断し、キューを一時停止しますか？\n\n%s\n\n途中出力を削除し、このジョブをキュー先頭に残します。",
			filepath.Base(job.Output),
		)).
		AddButtons([]string{"即時中断して一時停止", "戻る"}).
		SetDoneFunc(func(_ int, label string) {
			if label != "即時中断して一時停止" {
				u.showQueueJob(job)
				return
			}
			u.mu.Lock()
			cancel := u.cancel
			u.mu.Unlock()
			if cancel != nil {
				cancel()
			}
			u.showQueue()
		})
	modal.SetTitle(" 即時中断 ")
	modal.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEsc, tcell.KeyBackspace, tcell.KeyBackspace2:
			u.showQueueJob(job)
			return nil
		}
		return event
	})
	u.switchPage("queue-abort", modal)
}

func (u *UI) confirmDeleteQueueJob(job queue.Job) {
	modal := tview.NewModal().
		SetText(fmt.Sprintf(
			"キューから削除しますか？\n\n%s\nchapter %d-%d\n\n出力ファイル自体は削除しません。",
			filepath.Base(job.Output),
			job.ChapterStart,
			job.ChapterEnd,
		)).
		AddButtons([]string{"削除", "戻る"}).
		SetDoneFunc(func(_ int, label string) {
			if label != "削除" {
				u.showQueueJob(job)
				return
			}
			u.mu.Lock()
			running := u.running
			u.mu.Unlock()
			if running {
				q, err := u.service.Queue()
				if err != nil {
					u.showError("キュー削除", err, u.showQueue)
					return
				}
				if head, ok := q.Peek(); ok && head.ID == job.ID {
					u.showError("キュー削除", fmt.Errorf("実行中の先頭ジョブは削除できません"), u.showQueue)
					return
				}
			}
			if err := u.service.DeleteQueuedJob(job.ID); err != nil {
				u.showError("キュー削除", err, u.showQueue)
				return
			}
			if err := u.runner.DismissAlert(job.ID); err != nil {
				u.showError("異常状態の解除", err, u.showQueue)
				return
			}
			u.showQueue()
		})
	modal.SetTitle(" キュー削除 ")
	modal.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEsc, tcell.KeyBackspace, tcell.KeyBackspace2:
			u.showQueueJob(job)
			return nil
		}
		return event
	})
	u.switchPage("queue-delete", modal)
}

func (u *UI) startQueue() {
	u.mu.Lock()
	if u.running {
		u.mu.Unlock()
		u.showQueue()
		return
	}
	u.queuePaused = false
	u.pauseAfterCurrent = false
	u.mu.Unlock()

	q, err := u.service.Queue()
	if err != nil {
		u.showError("キュー実行", err, u.showMain)
		return
	}
	if len(q.Jobs) == 0 {
		u.showMessage("キュー実行", "キューは空です", u.showMain)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	u.mu.Lock()
	u.running = true
	u.cancel = cancel
	u.runSummary = ""
	u.currentJobID = q.Jobs[0].ID
	u.currentStage = "準備中"
	u.currentProgress = 0
	u.currentETA = 0
	u.currentDuration = time.Duration(q.Jobs[0].DurationSeconds) * time.Second
	u.currentStartedAt = time.Time{}
	u.currentPausedAt = time.Time{}
	u.currentPausedFor = 0
	u.encodingPaused = false
	u.speedFactor = 0
	u.lastRunError = ""
	u.mu.Unlock()

	u.runner.Stage = func(jobID, stage string) {
		u.terminal.QueueUpdateDraw(func() {
			u.mu.Lock()
			u.currentJobID = jobID
			u.currentStage = stage
			if stage == "handbrake" && u.currentStartedAt.IsZero() {
				u.currentStartedAt = time.Now()
				u.currentPausedAt = time.Time{}
				u.currentPausedFor = 0
				u.encodingPaused = false
				if q, err := u.service.Queue(); err == nil {
					for _, job := range q.Jobs {
						if job.ID == jobID {
							u.currentDuration = time.Duration(job.DurationSeconds) * time.Second
							break
						}
					}
				}
			} else if stage != "handbrake" {
				u.currentProgress = 0
				u.currentETA = 0
				u.currentStartedAt = time.Time{}
				u.currentPausedAt = time.Time{}
				u.currentPausedFor = 0
				u.encodingPaused = false
			}
			u.mu.Unlock()
			u.refreshQueuePage()
		})
	}
	u.runner.Progress = func(jobID string, progress handbrake.Progress) {
		u.terminal.QueueUpdateDraw(func() {
			u.mu.Lock()
			u.currentJobID = jobID
			u.currentStage = "handbrake"
			u.currentProgress = progress.Fraction
			u.currentETA = time.Duration(progress.ETASeconds) * time.Second
			activeElapsed := time.Since(u.currentStartedAt) - u.currentPausedFor
			if !u.currentPausedAt.IsZero() {
				activeElapsed -= time.Since(u.currentPausedAt)
			}
			if progress.Fraction > 0 && u.currentDuration > 0 && activeElapsed > 0 {
				u.speedFactor = float64(activeElapsed) / progress.Fraction / float64(u.currentDuration)
			}
			u.mu.Unlock()
			u.refreshQueuePage()
		})
	}
	u.runner.Completed = func(string) {
		u.terminal.QueueUpdateDraw(func() {
			page, _ := u.pages.GetFrontPage()
			if page == "queue-detail" || page == "queue-abort" {
				u.showQueue()
				return
			}
			u.refreshQueuePage()
		})
	}
	u.runner.PauseRequested = func() bool {
		u.mu.Lock()
		defer u.mu.Unlock()
		return u.pauseAfterCurrent
	}
	u.showQueue()

	go func() {
		result, runErr := u.runner.Run(ctx)
		remainingQueue, queueErr := u.service.Queue()
		hasRemaining := queueErr == nil && len(remainingQueue.Jobs) > 0
		runErrorText := ""
		canceled := false
		if runErr != nil {
			runErrorText = runErr.Error()
			var jobError *runner.JobError
			if errors.As(runErr, &jobError) {
				canceled = jobError.Canceled
				if jobError.LogPath != "" {
					runErrorText += " / log: " + jobError.LogPath
				}
			}
		}
		u.terminal.QueueUpdateDraw(func() {
			u.runner.Stage = nil
			u.runner.Progress = nil
			u.runner.Completed = nil
			u.runner.PauseRequested = nil
			u.mu.Lock()
			u.running = false
			u.cancel = nil
			u.currentJobID = ""
			u.currentStage = ""
			u.currentProgress = 0
			u.currentETA = 0
			u.currentDuration = 0
			u.currentStartedAt = time.Time{}
			u.currentPausedAt = time.Time{}
			u.currentPausedFor = 0
			u.encodingPaused = false
			u.speedFactor = 0
			if canceled {
				u.queuePaused = true
				u.runSummary = "即時中断により一時停止"
				u.lastRunError = ""
			} else if runErr != nil {
				u.queuePaused = true
				u.runSummary = "即時中断または失敗により一時停止"
				u.lastRunError = runErrorText
			} else if result.Paused && hasRemaining {
				u.queuePaused = true
				u.runSummary = fmt.Sprintf("%d件完了後に一時停止", result.Completed)
				u.lastRunError = ""
			} else {
				u.queuePaused = false
				u.runSummary = fmt.Sprintf("前回のキュー実行: %d件完了", result.Completed)
				u.lastRunError = ""
			}
			exitAfterCurrent := u.exitAfterCurrent
			u.exitAfterCurrent = false
			u.mu.Unlock()
			if exitAfterCurrent {
				u.terminal.Stop()
				return
			}
			page, _ := u.pages.GetFrontPage()
			switch page {
			case "queue", "queue-detail", "queue-abort":
				u.showQueue()
			case "main":
				u.showMain()
			}
		})
	}()
}

func (u *UI) startQueueAutomatically(added int) {
	u.mu.Lock()
	paused := u.queuePaused
	u.mu.Unlock()
	if paused {
		u.mu.Lock()
		u.runSummary = fmt.Sprintf("%d件追加（キューは一時停止中）", added)
		u.mu.Unlock()
		u.continueAdding()
		return
	}
	u.mu.Lock()
	running := u.running
	u.mu.Unlock()
	if !running {
		u.startQueue()
	}
	u.continueAdding()
}

func (u *UI) continueAdding() {
	u.addID++
	u.adding = true
	u.showFiles(filepath.Dir(u.draft.Input))
}

func (u *UI) refreshQueuePage() {
	page, _ := u.pages.GetFrontPage()
	switch page {
	case "queue":
		u.showQueueAt(u.queueSelection)
	case "main":
		u.showMain()
	}
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
	running := u.running
	u.mu.Unlock()
	if running {
		q, err := u.service.Queue()
		if err == nil {
			if head, ok := q.Peek(); ok {
				u.showQueueJob(head)
			}
		}
		return nil
	}
	u.terminal.Stop()
	return nil
}

func (u *UI) addActive(addID uint64) bool {
	return u.adding && u.addID == addID
}

type queueSnapshot struct {
	running         bool
	currentJobID    string
	currentStage    string
	currentProgress float64
	currentETA      time.Duration
	encodingPaused  bool
	speedFactor     float64
}

func (u *UI) queueSnapshotLocked() queueSnapshot {
	return queueSnapshot{
		running:         u.running,
		currentJobID:    u.currentJobID,
		currentStage:    u.currentStage,
		currentProgress: u.currentProgress,
		currentETA:      u.currentETA,
		encodingPaused:  u.encodingPaused,
		speedFactor:     u.speedFactor,
	}
}

func (u *UI) queueAlert() (runstate.State, error) {
	state, err := u.runner.Alert()
	if err != nil {
		return runstate.State{}, err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if state.Status == runstate.StatusIdle && u.lastRunError != "" {
		state.Status = runstate.StatusFailed
		state.Message = u.lastRunError
	}
	return state, nil
}

func formatQueueOverview(q queue.Queue, snapshot queueSnapshot) string {
	if len(q.Jobs) == 0 {
		return "キューは空です"
	}
	var text strings.Builder
	if eta, ok := estimateQueueETA(q, snapshot); ok {
		fmt.Fprintf(&text, "[yellow]全体ETA 約%s[white]\n", formatDuration(eta))
	}
	for i, job := range q.Jobs {
		status := "待機"
		if snapshot.running && job.ID == snapshot.currentJobID {
			status = fmt.Sprintf("実行中 %.1f%%", snapshot.currentProgress*100)
			if snapshot.encodingPaused {
				status = fmt.Sprintf("一時停止 %.1f%%", snapshot.currentProgress*100)
			}
		}
		fmt.Fprintf(
			&text,
			"%2d. %-16s  約%-8s  %s\n",
			i+1,
			status,
			formatJobDuration(job),
			filepath.Base(job.Output),
		)
	}
	return strings.TrimRight(text.String(), "\n")
}

func estimateQueueETA(q queue.Queue, snapshot queueSnapshot) (time.Duration, bool) {
	if !snapshot.running || snapshot.speedFactor <= 0 {
		return 0, false
	}
	current := -1
	for i, job := range q.Jobs {
		if job.ID == snapshot.currentJobID {
			current = i
			break
		}
	}
	if current < 0 {
		return 0, false
	}
	total := snapshot.currentETA
	currentDuration := q.Jobs[current].DurationSeconds
	switch snapshot.currentStage {
	case "starting", "準備中", "validate", "create-output-directory", "prepare-output", "scan", "resolve-preset", "build-handbrake-args":
		if currentDuration > 0 {
			total += time.Duration(float64(time.Duration(currentDuration)*time.Second) * snapshot.speedFactor)
		}
	}
	for _, job := range q.Jobs[current+1:] {
		duration := job.DurationSeconds
		if duration <= 0 {
			duration = currentDuration
		}
		if duration > 0 {
			total += time.Duration(float64(time.Duration(duration)*time.Second) * snapshot.speedFactor)
		}
	}
	if total < 0 {
		return 0, false
	}
	return total, true
}

func formatJobDuration(job queue.Job) string {
	if job.DurationSeconds <= 0 {
		return "--:--"
	}
	return formatDuration(time.Duration(job.DurationSeconds) * time.Second)
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

func withoutInt(values []int, excluded int) []int {
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value != excluded {
			result = append(result, value)
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

func outputDurationsForDisplay(
	chapters []media.Chapter,
	total time.Duration,
	selected []int,
	finalChapter int,
) ([]time.Duration, []bool) {
	if len(selected) == 0 || finalChapter < 1 {
		return make([]time.Duration, len(chapters)), make([]bool, len(chapters))
	}
	durations, available, err := media.OutputDurations(chapters, total, selected, finalChapter)
	if err != nil {
		return make([]time.Duration, len(chapters)), make([]bool, len(chapters))
	}
	return durations, available
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
