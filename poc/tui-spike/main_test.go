package main

import (
	"testing"

	"github.com/rivo/tview"
)

func TestBuildUIProvidesRequiredWidgetsAndPages(t *testing.T) {
	_, pages, form, menu := buildUI()

	if got, want := pages.GetPageCount(), 2; got != want {
		t.Fatalf("page count = %d, want %d", got, want)
	}
	if got, want := menu.GetItemCount(), 2; got != want {
		t.Fatalf("menu item count = %d, want %d", got, want)
	}
	if got, want := form.GetFormItemCount(), 3; got != want {
		t.Fatalf("form item count = %d, want %d", got, want)
	}
	if _, ok := form.GetFormItem(0).(*tview.Checkbox); !ok {
		t.Fatalf("form item 0 is %T, want *tview.Checkbox", form.GetFormItem(0))
	}
	if _, ok := form.GetFormItem(2).(*tview.InputField); !ok {
		t.Fatalf("form item 2 is %T, want *tview.InputField", form.GetFormItem(2))
	}

	pages.SwitchToPage("job")
	name, _ := pages.GetFrontPage()
	if name != "job" {
		t.Fatalf("front page = %q, want job", name)
	}
}
