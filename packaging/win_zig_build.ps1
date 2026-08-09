# win_build_fyne.ps1
# ------------------
# Fully working Windows GUI build for Kansho
# Embeds icon in EXE, stops CLI window, embeds version/commit/build time
# Fails hard if any required file is missing
# Uses zig as the cgo cross-compiler (CC/CXX)

$APP_NAME = "Kansho"
$OUTPUT_DIR = "../builds"         # relative to packaging/
$ICON_PNG = "./kansho.png"        # now relative to script location
$ICON_ICO = "./kansho.ico"
$RSRC_SYSO = "../rsrc.syso"       # output in root for go build

# ------------------
# Zig cgo compiler config
# ------------------
$ZIG_TARGET = "x86_64-windows-gnu"
$env:CC = "zig cc -target $ZIG_TARGET"
$env:CXX = "zig c++ -target $ZIG_TARGET"
$env:CGO_ENABLED = "1"
$env:GOOS = "windows"
$env:GOARCH = "amd64"

# Force the GUI subsystem explicitly at the linker level.
# Go's automatic translation of -H windowsgui into a linker flag
# does not reliably reach zig's lld the way it does with mingw-gcc,
# so we pass it directly via CGO_LDFLAGS as a belt-and-suspenders fix.
$env:CGO_LDFLAGS = "-Wl,--subsystem,windows"

# ------------------
# Check PNG exists
# ------------------
if (!(Test-Path $ICON_PNG)) {
    Write-Error "Error: PNG icon file '$ICON_PNG' not found."
    exit 1
}

# ------------------
# Convert PNG -> ICO
# ------------------
Write-Host "Converting PNG -> ICO..."
magick convert $ICON_PNG -define icon:auto-resize=256,128,64,48,32,16 $ICON_ICO
if (!(Test-Path $ICON_ICO)) {
    Write-Error "Error: Failed to generate ICO file."
    exit 1
}

# ------------------
# Generate rsrc.syso (embed icon in EXE)
# ------------------
Write-Host "Generating rsrc.syso..."
go install github.com/akavel/rsrc@latest
rsrc -ico $ICON_ICO -o $RSRC_SYSO
if (!(Test-Path $RSRC_SYSO)) {
    Write-Error "Error: Failed to generate rsrc.syso."
    exit 1
}

# ------------------
# Git / build info
# ------------------
$VERSION = git describe --tags --abbrev=0
$COMMIT = git rev-parse --short HEAD
$BUILD_TIME = (Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ")

# ------------------
# Prepare output folder
# ------------------
if (!(Test-Path $OUTPUT_DIR)) {
    New-Item -ItemType Directory -Path $OUTPUT_DIR | Out-Null
}

# ------------------
# Build GUI-only Windows executable
# ------------------
$LD_FLAGS = "-X kansho/config.Version=$VERSION -X kansho/config.GitCommit=$COMMIT -X kansho/config.BuildTime=$BUILD_TIME -H windowsgui"

Write-Host "Building $APP_NAME.exe..."

# Force a clean rebuild - a cached link from before the zig switch
# can otherwise silently mask whether this fix actually took effect.
go clean -cache

# Use root as build context, script is now in packaging/
go build -a -tags release -ldflags "$LD_FLAGS" -o "$OUTPUT_DIR\$APP_NAME.exe" ../

if (!(Test-Path "$OUTPUT_DIR\$APP_NAME.exe")) {
    Write-Error "Build failed: $OUTPUT_DIR\$APP_NAME.exe not found."
    exit 1
}

# ------------------
# Verify the subsystem actually got set to GUI
# ------------------
$peCheck = zig objdump -p "$OUTPUT_DIR\$APP_NAME.exe" 2>$null | Select-String -Pattern "Subsystem"
if ($peCheck) {
    Write-Host "PE Subsystem: $peCheck"
    if ($peCheck -notmatch "WINDOWS_GUI") {
        Write-Warning "Warning: built exe does NOT report WINDOWS_GUI subsystem. A console window will likely still appear."
    }
} else {
    Write-Warning "Could not verify PE subsystem (zig objdump unavailable or output format changed). Check manually if the console window still appears."
}

# ------------------
# Clean up temporary rsrc file
# ------------------
Remove-Item $RSRC_SYSO -Force

Write-Host "Build complete: $OUTPUT_DIR\$APP_NAME.exe"
Write-Host "GUI-only, icon embedded, version info embedded. Built via zig ($ZIG_TARGET)."