APP_NAME := kansho
LOGVIEWER_NAME := kansho-logviewer
VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "0.1.0")
CLEAN_VERSION := $(shell echo $(VERSION) | sed 's/^v//')

GOOS := linux
GOARCH := amd64

ROOTFS := rootfs
DEBIAN_DIR := $(ROOTFS)/DEBIAN
BIN_DIR := $(ROOTFS)/usr/bin
DESKTOP_DIR := $(ROOTFS)/usr/share/applications
ICON_DIR := $(ROOTFS)/usr/share/icons/hicolor/256x256/apps

all: deb

deb: clean prepare build install_files package

prepare:
	@echo "==> Preparing rootfs directories"
	mkdir -p $(DEBIAN_DIR) $(BIN_DIR) $(DESKTOP_DIR) $(ICON_DIR)

	@echo "==> Generating DEBIAN/control"
	sed "s/@VERSION@/$(CLEAN_VERSION)/" packaging/DEBIAN/control.in > $(DEBIAN_DIR)/control

	@echo "==> Copying desktop and icon files"
	cp packaging/kansho.desktop $(DESKTOP_DIR)/$(APP_NAME).desktop
	cp packaging/kansho.png $(ICON_DIR)/$(APP_NAME).png

build:
	@echo "==> Building main application binary"
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=1 \
	go build -tags release \
	-ldflags "-X kansho/config.Version=$(VERSION) -X kansho/config.GitCommit=$(shell git rev-parse --short HEAD)" \
	-o $(BIN_DIR)/$(APP_NAME) \
	./cmd/kansho

	@echo "==> Building log viewer binary"
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=1 \
	go build -tags release \
	-o $(BIN_DIR)/$(LOGVIEWER_NAME) \
	./cmd/kansho-logviewer

install_files:
	@echo "==> Setting permissions"
	chmod 755 $(BIN_DIR)/$(APP_NAME)
	chmod 755 $(BIN_DIR)/$(LOGVIEWER_NAME)
	chmod 644 $(DESKTOP_DIR)/$(APP_NAME).desktop
	chmod 644 $(ICON_DIR)/$(APP_NAME).png
	chmod 755 $(DEBIAN_DIR)

package:
	@echo "==> Building Debian package"
	dpkg-deb --build $(ROOTFS) $(APP_NAME)_$(VERSION)_amd64.deb
	@echo "==> Package created: $(APP_NAME)_$(VERSION)_amd64.deb"

clean:
	@echo "==> Cleaning up"
	rm -rf bin/
	rm -rf $(ROOTFS)
	rm -f $(APP_NAME)_*.deb

# Development targets
dev-main:
	@echo "==> Building main app for development"
	go build -o bin/$(APP_NAME) ./cmd/kansho

dev-logviewer:
	@echo "==> Building log viewer for development"
	go build -o bin/$(LOGVIEWER_NAME) ./cmd/kansho-logviewer

dev: dev-main dev-logviewer
	@echo "==> Development binaries built in bin/"

.PHONY: all deb prepare build install_files package clean dev dev-main dev-logviewer