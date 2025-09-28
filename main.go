package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx context.Context
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// OnStartup is called when the app starts up
func (a *App) OnStartup(ctx context.Context) {
	a.ctx = ctx
}

// Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}

// GetCurrentTime returns the current time
func (a *App) GetCurrentTime() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

// ShowMessage displays a message dialog
func (a *App) ShowMessage(title, message string) {
	runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:    runtime.InfoDialog,
		Title:   title,
		Message: message,
	})
}

// ShowOpenDialog opens a file dialog
func (a *App) ShowOpenDialog() string {
	selection, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select a file",
		Filters: []runtime.FileFilter{
			{
				DisplayName: "Text Files (*.txt)",
				Pattern:     "*.txt",
			},
			{
				DisplayName: "All Files (*.*)",
				Pattern:     "*.*",
			},
		},
	})
	if err != nil {
		return "Error: " + err.Error()
	}
	if selection == "" {
		return "No file selected"
	}
	return "Selected: " + selection
}

// GetSystemInfo returns basic system information
func (a *App) GetSystemInfo() map[string]string {
	info := make(map[string]string)
	info["OS"] = runtime.Environment(a.ctx).Platform
	info["Arch"] = runtime.Environment(a.ctx).Arch
	return info
}

// Menu callback functions
func (a *App) onNewFile(_ *menu.CallbackData) {
	runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:    runtime.InfoDialog,
		Title:   "Menu Action",
		Message: "New file clicked!",
	})
}

func (a *App) onOpenFile(_ *menu.CallbackData) {
	runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Open File",
	})
}

func (a *App) onExit(_ *menu.CallbackData) {
	runtime.Quit(a.ctx)
}

func (a *App) onCopy(_ *menu.CallbackData) {
	runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:    runtime.InfoDialog,
		Title:   "Menu Action",
		Message: "Copy clicked!",
	})
}

func (a *App) onPaste(_ *menu.CallbackData) {
	runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:    runtime.InfoDialog,
		Title:   "Menu Action",
		Message: "Paste clicked!",
	})
}

func (a *App) onAbout(_ *menu.CallbackData) {
	runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:    runtime.InfoDialog,
		Title:   "About",
		Message: "Wails GUI Example v1.0\nBuilt with Wails v2",
	})
}

// CreateApplicationMenu creates the application menu
func (a *App) createMenu() *menu.Menu {
	appMenu := menu.NewMenu()

	// File menu
	fileMenu := appMenu.AddSubmenu("File")
	fileMenu.AddText("New", keys.CmdOrCtrl("n"), a.onNewFile)
	fileMenu.AddText("Open", keys.CmdOrCtrl("o"), a.onOpenFile)
	fileMenu.AddSeparator()
	fileMenu.AddText("Exit", keys.CmdOrCtrl("q"), a.onExit)

	// Edit menu
	editMenu := appMenu.AddSubmenu("Edit")
	editMenu.AddText("Copy", keys.CmdOrCtrl("c"), a.onCopy)
	editMenu.AddText("Paste", keys.CmdOrCtrl("v"), a.onPaste)

	// Help menu
	helpMenu := appMenu.AddSubmenu("Help")
	helpMenu.AddText("About", nil, a.onAbout)

	return appMenu
}

func main() {
	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	err := wails.Run(&options.App{
		Title:  "Wails GUI Example",
		Width:  800,
		Height: 600,
		AssetServer: &assetserver.Options{
			Assets: os.DirFS("frontend/dist"),
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		Menu:             app.createMenu(),
		OnStartup:        app.OnStartup,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		log.Fatal(err)
	}
}