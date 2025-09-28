# Wails GUI Example

A cross-platform desktop application built with Go and Wails v2, featuring a modern web-based UI with buttons, menus, and interactive elements.

## Features

- **Modern UI**: Clean, responsive interface with gradient backgrounds and smooth animations
- **Interactive Components**: Buttons, progress bars, input fields, and counters
- **System Integration**: File dialogs, message boxes, and system information
- **Menu System**: Native application menus with keyboard shortcuts
- **Cross-Platform**: Runs on Windows, macOS, and Linux

## Project Structure

```
wails-gui-example/
├── main.go                 # Go backend with Wails app logic
├── go.mod                  # Go module dependencies
├── wails.json             # Wails configuration
└── frontend/              # Frontend assets
    ├── index.html         # Main HTML interface
    ├── style.css          # Modern CSS with animations
    ├── main.js            # JavaScript frontend logic
    ├── package.json       # Frontend build configuration
    └── dist/              # Built frontend assets (auto-generated)
```

## Prerequisites

1. **Go** (1.21 or later)
2. **Node.js** (14 or later) 
3. **Wails CLI** - Install with:
   ```bash
   go install github.com/wailsapp/wails/v2/cmd/wails@latest
   ```

## Quick Start

1. **Create the project directory**:
   ```bash
   mkdir wails-gui-example
   cd wails-gui-example
   ```

2. **Create the files**: Copy all the provided code files into their respective locations:
   - `main.go` in the root directory
   - `go.mod` in the root directory  
   - `wails.json` in the root directory
   - Create `frontend/` directory and add the frontend files

3. **Initialize the frontend**:
   ```bash
   cd frontend
   npm install
   cd ..
   ```

4. **Initialize Go modules**:
   ```bash
   go mod tidy
   ```

5. **Run in development mode**:
   ```bash
   wails dev
   ```

6. **Build for production**:
   ```bash
   wails build
   ```

## Application Features

### Backend Functions (Go)

- **Greet(name string)**: Returns personalized greeting
- **GetCurrentTime()**: Returns formatted current time
- **GetSystemInfo()**: Returns OS and architecture info
- **ShowMessage()**: Displays native message dialog
- **ShowOpenDialog()**: Opens native file picker dialog

### Frontend Features

- **Greeting Section**: Interactive name input with greeting functionality
- **System Information**: Time and system info display
- **Dialog Examples**: Native OS dialogs for messages and file selection
- **Interactive Demo**: Click counter and animated progress bar
- **Keyboard Shortcuts**: 
  - Ctrl/Cmd + G: Trigger greeting
  - Ctrl/Cmd + T: Get current time
  - Space: Increment counter

### Menu System

- **File Menu**: New, Open, Exit with keyboard shortcuts
- **Edit Menu**: Copy, Paste operations
- **Help Menu**: About dialog with app information

## Development Notes

### Frontend Development

The frontend uses vanilla HTML, CSS, and JavaScript for maximum compatibility. The `main.js` includes development mode fallbacks that mock the Go backend functions when `window.go` is not available.

### Styling

The CSS features:
- Modern gradient backgrounds
- Smooth transitions and hover effects  
- Responsive grid layout
- Custom button styles with ripple effects
- Animated progress bars
- Mobile-responsive design

### Go Backend

The Go backend provides:
- Context-aware operations
- Native OS integration
- Menu system with callbacks
- File system access
- Runtime environment info

## Building for Different Platforms

### Windows
```bash
wails build -platform windows/amd64
```

### macOS  
```bash
wails build -platform darwin/amd64
wails build -platform darwin/arm64
```

### Linux
```bash
wails build -platform linux/amd64
```

## Customization

### Adding New Backend Functions

1. Add method to the `App` struct in `main.go`
2. The function will be automatically available as `window.go.main.App.YourFunction()` in JavaScript

### Adding New UI Components

1. Add HTML structure to `index.html`
2. Style in `style.css` 
3. Add interactivity in `main.js`

### Modifying Menus

Edit the `createMenu()` function in `main.go` to add new menu items, shortcuts, or callbacks.

## Troubleshooting

### Common Issues

1. **"wails: command not found"**: Install Wails CLI with the command above
2. **Frontend build fails**: Ensure Node.js is installed and run `npm install` in frontend directory
3. **Go modules issues**: Run `go mod tidy` to resolve dependencies

### Development Mode

Use `wails dev` for hot-reloading during development. The frontend includes mock functions for testing without the Go backend.

## License

This project is provided as example code for learning Wails development.