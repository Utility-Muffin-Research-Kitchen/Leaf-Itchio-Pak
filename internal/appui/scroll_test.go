package appui

import "testing"

func TestBodyScrollStaysWithinBounds(t *testing.T) {
	var scroll BodyScroll
	if scroll.HandleScroll(ButtonDown); scroll.ScrollLine != 0 {
		t.Fatalf("scrolled to %d before the body was drawn", scroll.ScrollLine)
	}
	scroll.SetScrollBounds(2)
	for range 3 {
		scroll.HandleScroll(ButtonDown)
	}
	if scroll.ScrollLine != 2 {
		t.Fatalf("Down x3 = line %d, want 2", scroll.ScrollLine)
	}
	scroll.SetScrollBounds(1)
	if scroll.ScrollLine != 1 {
		t.Fatalf("shrunk bounds left line %d, want 1", scroll.ScrollLine)
	}
	for range 3 {
		scroll.HandleScroll(ButtonUp)
	}
	if scroll.ScrollLine != 0 {
		t.Fatalf("Up x3 = line %d, want 0", scroll.ScrollLine)
	}
	if scroll.HandleScroll(ButtonA) {
		t.Fatal("A counted as a scroll button")
	}
	scroll.SetScrollBounds(-4)
	if scroll.ScrollMax != 0 {
		t.Fatalf("negative bound = %d, want 0", scroll.ScrollMax)
	}
	scroll.SetScrollBounds(3)
	scroll.HandleScroll(ButtonDown)
	scroll.ResetScroll()
	if scroll.ScrollLine != 0 {
		t.Fatalf("reset left line %d", scroll.ScrollLine)
	}
}

// scrollsOnce checks that Down moves the body one line, Right a page, and
// that neither returns an intent, so scrolling never closes or confirms a
// screen.
func scrollsOnce[I comparable](t *testing.T, name string, scroll *BodyScroll, handle func(InputEvent) I) {
	t.Helper()
	var none I
	// Left and Right page by the lines that show at once, less one for
	// context, and never close or confirm a screen either.
	scroll.SetScrollBounds(30)
	scroll.SetScrollRows(8)
	if got := handle(press(ButtonRight)); got != none {
		t.Fatalf("%s: Right intent = %v, want none", name, got)
	}
	if scroll.ScrollLine != 7 {
		t.Fatalf("%s: Right paged to line %d, want 7", name, scroll.ScrollLine)
	}
	if got := handle(press(ButtonLeft)); got != none {
		t.Fatalf("%s: Left intent = %v, want none", name, got)
	}
	if scroll.ScrollLine != 0 {
		t.Fatalf("%s: Left paged back to line %d, want 0", name, scroll.ScrollLine)
	}
	scroll.SetScrollBounds(3)
	if got := handle(press(ButtonDown)); got != none {
		t.Fatalf("%s: Down intent = %v, want none", name, got)
	}
	if scroll.ScrollLine != 1 {
		t.Fatalf("%s: Down scrolled to line %d, want 1", name, scroll.ScrollLine)
	}
	if got := handle(press(ButtonUp)); got != none {
		t.Fatalf("%s: Up intent = %v, want none", name, got)
	}
	if scroll.ScrollLine != 0 {
		t.Fatalf("%s: Up scrolled to line %d, want 0", name, scroll.ScrollLine)
	}
	scroll.HandleScroll(ButtonDown)
}

func TestManagePromptAndResultScroll(t *testing.T) {
	model := NewManageModel("Leafbound")
	model.SetConfirm("Delete 31 managed files?", []BodyBlock{ListBlock([]ListEntry{
		{Text: "01 Theme.ogg", Detail: "Primary SD / Music/01 Theme.ogg"}})})
	scrollsOnce(t, "confirm", &model.BodyScroll, model.Handle)
	if got := model.Handle(press(ButtonA)); got != ManageIntentConfirm {
		t.Fatalf("confirm A = %v, want confirm", got)
	}
	if got := model.Handle(press(ButtonB)); got != ManageIntentCancel {
		t.Fatalf("confirm B = %v, want cancel", got)
	}
	model.SetConfirm("Delete selected file?", Prose("Leafbound.gbc"))
	if model.ScrollLine != 0 {
		t.Fatalf("new prompt kept line %d", model.ScrollLine)
	}
	model.SetResult("Deleted 31 managed file(s).")
	if model.ScrollLine != 0 {
		t.Fatal("result kept the prompt's scroll line")
	}
	scrollsOnce(t, "result", &model.BodyScroll, model.Handle)
	model.SetLibraryStatus("Leaf library rescan queued.")
	if model.ScrollLine != 1 {
		t.Fatalf("library status moved the result to line %d", model.ScrollLine)
	}
	if got := model.Handle(press(ButtonA)); got != ManageIntentBack {
		t.Fatalf("result A = %v, want back", got)
	}
	model.SetItems("1 managed file", []ManageItem{{Label: "a"}, {Label: "b"}})
	model.Handle(press(ButtonDown))
	if model.Cursor != 1 || model.ScrollLine != 0 {
		t.Fatalf("list Down = cursor %d line %d, want the cursor to move", model.Cursor, model.ScrollLine)
	}
}

func TestSettingsPromptScrolls(t *testing.T) {
	model := NewSettingsModel("Settings")
	model.SetRows("", []SettingsRow{{Label: "a"}, {Label: "b"}})
	model.SetConfirm("Sign out of itch.io?", []string{"Owned-game data on this device is cleared."})
	scrollsOnce(t, "confirm", &model.BodyScroll, model.Handle)
	if got := model.Handle(press(ButtonA)); got != SettingsIntentConfirm {
		t.Fatalf("confirm A = %v, want confirm", got)
	}
	if got := model.Handle(press(ButtonB)); got != SettingsIntentCancel {
		t.Fatalf("confirm B = %v, want cancel", got)
	}
	model.SetConfirm("Reset remembered folders?", nil)
	if model.ScrollLine != 0 {
		t.Fatalf("new prompt kept line %d", model.ScrollLine)
	}
}

func TestDownloadScreensScroll(t *testing.T) {
	done := &DownloadProgressModel{State: DownloadProgressDone, SavedPaths: []string{"a.ogg"}}
	scrollsOnce(t, "done", &done.BodyScroll, done.Handle)
	if got := done.Handle(press(ButtonA)); got != DownloadProgressIntentBack {
		t.Fatalf("done A = %v, want back", got)
	}
	blocked := &DownloadProgressModel{State: DownloadProgressInhibitBlocked}
	scrollsOnce(t, "inhibit", &blocked.BodyScroll, blocked.Handle)
	if got := blocked.Handle(press(ButtonA)); got != DownloadProgressIntentContinue {
		t.Fatalf("inhibit A = %v, want continue", got)
	}
	if got := blocked.Handle(press(ButtonB)); got != DownloadProgressIntentBack {
		t.Fatalf("inhibit B = %v, want back", got)
	}

	selection := NewDownloadSelectModel("Leafbound")
	selection.SetError("Can't reach itch.io. Check the connection and try again.")
	scrollsOnce(t, "select error", &selection.BodyScroll, selection.Handle)
	if got := selection.Handle(press(ButtonA)); got != DownloadSelectIntentBack {
		t.Fatalf("select error A = %v, want back", got)
	}
	selection.SetHandoff("Choose where to save the soundtrack.")
	if selection.ScrollLine != 0 {
		t.Fatalf("handoff kept line %d", selection.ScrollLine)
	}
	scrollsOnce(t, "handoff", &selection.BodyScroll, selection.Handle)
	selection.SetError("Another error.")
	if selection.ScrollLine != 0 {
		t.Fatalf("new error kept line %d", selection.ScrollLine)
	}
}

// The progress screen takes a new snapshot of the download on every pass.
// The body keeps its place while the state stays, and starts over when it
// changes.
func TestDownloadProgressSnapshotKeepsScrollInTheSameState(t *testing.T) {
	model := &DownloadProgressModel{State: DownloadProgressDone, SavedPaths: []string{"a.ogg"}}
	model.SetScrollBounds(4)
	model.Handle(press(ButtonDown))
	model.Handle(press(ButtonDown))
	model.Apply(DownloadProgressModel{State: DownloadProgressDone, SavedPaths: []string{"a.ogg"},
		LibraryStatus: "Leaf library rescan requested."})
	if model.ScrollLine != 2 || model.LibraryStatus == "" {
		t.Fatalf("same-state snapshot = line %d status %q, want line 2 and the new status",
			model.ScrollLine, model.LibraryStatus)
	}
	model.Apply(DownloadProgressModel{State: DownloadProgressError, Detail: "Download failed."})
	if model.ScrollLine != 0 || model.State != DownloadProgressError {
		t.Fatalf("new-state snapshot = line %d state %v, want line 0", model.ScrollLine, model.State)
	}
}

func TestDestinationPromptsScroll(t *testing.T) {
	model := NewDestinationModel("Leafbound")
	model.SetConfirm("Confirm download destination", "Primary SD", []BodyBlock{
		ListBlock([]ListEntry{{Text: "Roms/GBC/Leafbound.gbc"}}), Paragraph("Roms/GBC")})
	scrollsOnce(t, "confirm", &model.BodyScroll, model.Handle)
	if got := model.Handle(press(ButtonA)); got != DestinationIntentActivate {
		t.Fatalf("confirm A = %v, want activate", got)
	}
	model.SetError("The SD card was removed.")
	if model.ScrollLine != 0 {
		t.Fatalf("error kept line %d", model.ScrollLine)
	}
	scrollsOnce(t, "error", &model.BodyScroll, model.Handle)
	if got := model.Handle(press(ButtonA)); got != DestinationIntentBack {
		t.Fatalf("error A = %v, want back", got)
	}
	model.SetConfirm("Confirm download destination", "Primary SD", nil)
	if model.ScrollLine != 0 {
		t.Fatalf("new prompt kept line %d", model.ScrollLine)
	}
}

func TestRenameCompleteScrolls(t *testing.T) {
	model := NewRenameModel("Leafbound")
	model.SetPrompt(RenameConfirmSaves, "Save files", "Rename these save files?", []ListEntry{{Text: "a", Detail: "b"}})
	model.SetScrollBounds(2)
	model.Handle(press(ButtonDown))
	model.SetDone("ROM renamed, 1 save, 2 state files.")
	if model.ScrollLine != 0 {
		t.Fatalf("result kept the prompt's line %d", model.ScrollLine)
	}
	scrollsOnce(t, "done", &model.BodyScroll, model.Handle)
	if got := model.Handle(press(ButtonB)); got != RenameIntentBack {
		t.Fatalf("done B = %v, want back", got)
	}
}

func TestSignInWarningAndCodeScroll(t *testing.T) {
	model := &SignInModel{State: SignInWarning}
	scrollsOnce(t, "warning", &model.BodyScroll, model.Handle)
	if got := model.Handle(press(ButtonA)); got != SignInIntentAccept {
		t.Fatalf("warning A = %v, want accept", got)
	}
	if got := model.Handle(press(ButtonB)); got != SignInIntentBack {
		t.Fatalf("warning B = %v, want back", got)
	}
	code := &SignInModel{State: SignInWaiting, UserCode: "KXR4-7PLM"}
	scrollsOnce(t, "code", &code.BodyScroll, code.Handle)
	if got := code.Handle(press(ButtonB)); got != SignInIntentCancel {
		t.Fatalf("code B = %v, want cancel", got)
	}
}

func TestDetailStartsANewBodyAtTheTop(t *testing.T) {
	model := NewDetailModel(DetailGame{Title: "Leafbound", Downloaded: true})
	model.SetReady("<p>One</p><p>Two</p>", nil, nil, false, nil)
	model.SetScrollBounds(3)
	model.Handle(press(ButtonDown))
	model.SetError("Can't reach itch.io. Check the connection, then reopen this game.")
	if model.ScrollLine != 0 {
		t.Fatalf("unavailable page kept line %d", model.ScrollLine)
	}
	scrollsOnce(t, "unavailable", &model.BodyScroll, model.Handle)
}

func TestBodyScrollPagesByTheVisibleLines(t *testing.T) {
	var scroll BodyScroll
	scroll.SetScrollBounds(20)
	scroll.SetScrollRows(8)
	// A page is the 8 visible lines less one, so the last line read stays
	// on screen after the page.
	for _, want := range []int{7, 14, 20, 20} {
		if !scroll.HandleScroll(ButtonRight) || scroll.ScrollLine != want {
			t.Fatalf("Right = line %d, want %d (clamped to the last)", scroll.ScrollLine, want)
		}
	}
	for _, want := range []int{13, 6, 0, 0} {
		if !scroll.HandleScroll(ButtonLeft) || scroll.ScrollLine != want {
			t.Fatalf("Left = line %d, want %d (clamped to the first)", scroll.ScrollLine, want)
		}
	}
	// A body that shows one line pages by one, and so does one that has not
	// been drawn yet.
	scroll.SetScrollRows(1)
	scroll.HandleScroll(ButtonRight)
	scroll.SetScrollRows(0)
	scroll.HandleScroll(ButtonRight)
	if scroll.ScrollLine != 2 {
		t.Fatalf("one-line pages reached line %d, want 2", scroll.ScrollLine)
	}
	// Paging stays within a body that fits.
	scroll.SetScrollBounds(0)
	scroll.SetScrollRows(8)
	scroll.HandleScroll(ButtonRight)
	if scroll.ScrollLine != 0 {
		t.Fatalf("Right in a body that fits = line %d, want 0", scroll.ScrollLine)
	}
}

// The rename prompts scroll with Up and Down and page with Left and Right,
// and no scroll button confirms or skips.
func TestRenamePromptPages(t *testing.T) {
	model := NewRenameModel("Leafbound")
	model.SetPrompt(RenameConfirmStates, "Save states", "Rename these state files?", []ListEntry{{Text: "a", Detail: "b"}})
	model.SetScrollBounds(30)
	model.SetScrollRows(8)
	for _, step := range []struct {
		button Button
		line   int
	}{{ButtonDown, 1}, {ButtonRight, 8}, {ButtonLeft, 1}, {ButtonUp, 0}} {
		if got := model.Handle(press(step.button)); got != RenameIntentNone {
			t.Fatalf("button %v intent = %v, want none", step.button, got)
		}
		if model.ScrollLine != step.line {
			t.Fatalf("button %v = line %d, want %d", step.button, model.ScrollLine, step.line)
		}
	}
}

// Detail keeps Left and Right (and L1 and R1) for the images; only Up and
// Down scroll the description there.
func TestDetailLeftRightStillMoveTheImages(t *testing.T) {
	model := NewDetailModel(DetailGame{Title: "Leafbound"})
	model.SetReady("<p>One</p><p>Two</p>", nil, []string{"cover", "shot", "shot2"}, false, nil)
	model.SetScrollBounds(30)
	model.SetScrollRows(8)
	for _, step := range []struct {
		button Button
		image  int
	}{{ButtonRight, 1}, {ButtonR1, 2}, {ButtonLeft, 1}, {ButtonL1, 0}} {
		model.Handle(press(step.button))
		if model.ImageIndex != step.image || model.ScrollLine != 0 {
			t.Fatalf("button %v = image %d line %d, want image %d and no scroll",
				step.button, model.ImageIndex, model.ScrollLine, step.image)
		}
	}
	model.Handle(press(ButtonDown))
	if model.ScrollLine != 1 || model.ImageIndex != 0 {
		t.Fatalf("Down = line %d image %d, want line 1 and the same image", model.ScrollLine, model.ImageIndex)
	}
}
