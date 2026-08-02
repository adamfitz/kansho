package ui

import (
	"fyne.io/fyne/v2"
)

// greenTickResource is a green checkmark used to indicate that a chapter has
// been downloaded to disk. It is rendered through widget.Icon so it has no
// button chrome and no hover highlight.
var greenTickResource = &fyne.StaticResource{
	StaticName:    "green_tick.svg",
	StaticContent: []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><path d="M5 12.5 L10 17.5 L19 7.5" fill="none" stroke="#43f436" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"/></svg>`),
}

// redNoEntryResource is a red circle-with-slash icon used for the per-chapter
// action that either cancels an active download or deletes the local file.
var redNoEntryResource = &fyne.StaticResource{
	StaticName:    "red_no_entry.svg",
	StaticContent: []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><circle cx="12" cy="12" r="9" fill="none" stroke="#f44336" stroke-width="3"/><path d="M6.5 6.5 L17.5 17.5" fill="none" stroke="#f44336" stroke-width="3" stroke-linecap="round"/></svg>`),
}
