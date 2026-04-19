APP_NAME := kansho
GOARCH   := amd64
BINARY   := $(APP_NAME)

# ---------------------------------------------------------------------------
# OS detection — must be first, before any $(shell ...) calls
# ---------------------------------------------------------------------------
ifeq ($(OS),Windows_NT)
  BUILD_OS := windows
else
  BUILD_OS := linux
  VERSION       := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "0.1.0")
  CLEAN_VERSION := $(shell echo $(VERSION) | sed 's/^v//')
endif

ROOTFS       := rootfs
DEBIAN_DIR   := $(ROOTFS)/DEBIAN
BIN_DIR      := $(ROOTFS)/usr/bin
DESKTOP_DIR  := $(ROOTFS)/usr/share/applications
ICON_DIR     := $(ROOTFS)/usr/share/icons/hicolor/256x256/apps

# ---------------------------------------------------------------------------
# Default — show help
# ---------------------------------------------------------------------------
.DEFAULT_GOAL := help

.PHONY: help
help:
	@echo ""
	@echo "  kansho build"
	@echo "  Detected OS: $(BUILD_OS)"
	@echo ""
	@echo "  make linux    Build Debian package (Linux only)"
	@echo "  make windows  Build Windows exe    (Windows only)"
	@echo "  make clean    Remove build artefacts"
	@echo ""

# ---------------------------------------------------------------------------
# Linux — identical logic to the original Makefile, targets just renamed
# ---------------------------------------------------------------------------
.PHONY: linux
linux:
ifneq ($(BUILD_OS),linux)
	$(error 'make linux' must be run on Linux)
endif
linux: clean prepare build install_files package

.PHONY: prepare
prepare:
	@echo "==> Preparing rootfs directories"
	mkdir -p $(DEBIAN_DIR) $(BIN_DIR) $(DESKTOP_DIR) $(ICON_DIR)
	@echo "==> Generating DEBIAN/control"
	sed "s/@VERSION@/$(CLEAN_VERSION)/" packaging/DEBIAN/control.in > $(DEBIAN_DIR)/control
	@echo "==> Copying desktop and icon files"
	cp packaging/kansho.desktop $(DESKTOP_DIR)/$(APP_NAME).desktop
	cp packaging/kansho.png     $(ICON_DIR)/$(APP_NAME).png

.PHONY: build
build:
	@echo "==> Building Go binary (linux/amd64)"
	GOOS=linux GOARCH=$(GOARCH) CGO_ENABLED=1 \
	go build -tags release \
	  -ldflags "-X kansho/config.Version=$(VERSION) \
	            -X kansho/config.GitCommit=$(shell git rev-parse --short HEAD) \
	            -X kansho/config.BuildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)" \
	  -o $(BIN_DIR)/$(BINARY) .

.PHONY: install_files
install_files:
	@echo "==> Setting permissions"
	chmod 755 $(BIN_DIR)/$(BINARY)
	chmod 644 $(DESKTOP_DIR)/$(APP_NAME).desktop
	chmod 644 $(ICON_DIR)/$(APP_NAME).png
	chmod 755 $(DEBIAN_DIR)

.PHONY: package
package:
	@echo "==> Building Debian package"
	dpkg-deb --build $(ROOTFS) $(APP_NAME)_$(VERSION)_amd64.deb
	@echo "==> Package created: $(APP_NAME)_$(VERSION)_amd64.deb"

.PHONY: clean
clean:
ifeq ($(BUILD_OS),linux)
	@echo "==> Cleaning up"
	rm -rf $(ROOTFS)
	rm -f $(APP_NAME)_*.deb
else
	@echo "==> Cleaning up"
	powershell -ExecutionPolicy Bypass -Command "if (Test-Path builds) { Remove-Item builds -Recurse -Force }"
endif

# ---------------------------------------------------------------------------
# Windows — delegates entirely to the existing PowerShell script
# ---------------------------------------------------------------------------
.PHONY: windows
windows:
ifneq ($(BUILD_OS),windows)
	$(error 'make windows' must be run on Windows)
endif
	@echo "==> Building Windows exe via PowerShell..."
	powershell -ExecutionPolicy Bypass -Command "Set-Location 'packaging'; & '.\win_build_fyne.ps1'"