// Command tui-spike is a disposable Milestone 0 proof of tview widgets.
// It is not ChapterBrake product code.
package main

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func buildUI() (*tview.Application, *tview.Pages, *tview.Form, *tview.List) {
	app := tview.NewApplication()
	pages := tview.NewPages()

	menu := tview.NewList().
		AddItem("新しいジョブを追加", "", 'a', func() {
			pages.SwitchToPage("job")
		}).
		AddItem("終了", "", 'q', app.Stop)

	form := tview.NewForm().
		AddCheckbox("チャプター 1", true, nil).
		AddCheckbox("チャプター 2", false, nil).
		AddInputField("出力ベース名", "source", 40, nil, nil).
		AddButton("戻る", func() {
			pages.SwitchToPage("menu")
		})

	pages.
		AddPage("menu", menu, true, true).
		AddPage("job", form, true, false)

	app.SetRoot(pages, true)
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlC {
			app.Stop()
			return nil
		}
		return event
	})
	return app, pages, form, menu
}

func main() {
	app, _, _, _ := buildUI()
	if err := app.Run(); err != nil {
		panic(err)
	}
}
