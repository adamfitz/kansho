APP_NAME := kansho
VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "0.1.0")
CLEAN_VERSION := $(shell echo $(VERSION) | sed 's/^v//')

GOOS := linux
GOARCH := amd64
BINARY := $(APP_NAME)

ROOTFS := rootfs
DEBIAN_DIR := $(ROOTFS)/DEBIAN
BIN_DIR := $(ROOTFS)/usr/bin
DESKTOP_DIR := $(ROOTFS)/usr/share/applications
ICON_DIR := $(ROOTFS)/usr/share/icons/hicolor/256x256/apps

all: deb

deb: clean download-rlv prepare build install_files package

download-rlv:
	@echo "==> Resolving latest rlv version"
	RLV_VERSION=$$(curl -sL https://api.github.com/repos/adamfitz/rlv/releases/latest | grep '"tag_name"' | cut -d'"' -f4) && \
	RLV_DEB="rlv_$${RLV_VERSION#v}_amd64.deb" && \
	echo "==> Downloading rlv $$RLV_VERSION" && \
	curl -sL "https://github.com/adamfitz/rlv/releases/download/$$RLV_VERSION/$$RLV_DEB" -o "/tmp/$$RLV_DEB" && \
	echo "==> Extracting rlv binary" && \
	mkdir -p /tmp/rlv-extract && \
	dpkg-deb --extract "/tmp/$$RLV_DEB" /tmp/rlv-extract && \
	cp /tmp/rlv-extract/usr/bin/rlv . && \
	chmod 755 rlv && \
	rm -rf "/tmp/$$RLV_DEB" /tmp/rlv-extract

prepare:
	@echo "==> Preparing rootfs directories"
	mkdir -p $(DEBIAN_DIR) $(BIN_DIR) $(DESKTOP_DIR) $(ICON_DIR)

	@echo "==> Generating DEBIAN/control"
	sed "s/@VERSION@/$(CLEAN_VERSION)/" packaging/DEBIAN/control.in > $(DEBIAN_DIR)/control

	@echo "==> Copying desktop and icon files"
	cp packaging/kansho.desktop $(DESKTOP_DIR)/$(APP_NAME).desktop
	cp packaging/kansho.png $(ICON_DIR)/$(APP_NAME).png

build:
	@echo "==> Building Go binary"
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=1 \
	go build -tags release -ldflags "-X github.com/backyard/kansho/config.GitCommit=$(git rev-parse --short HEAD)" -o $(BIN_DIR)/$(BINARY) .

install_files:
	@echo "==> Installing rlv binary"
	cp rlv $(BIN_DIR)/rlv
	@echo "==> Setting permissions"
	chmod 755 $(BIN_DIR)/$(BINARY)
	chmod 755 $(BIN_DIR)/rlv
	chmod 644 $(DESKTOP_DIR)/$(APP_NAME).desktop
	chmod 644 $(ICON_DIR)/$(APP_NAME).png
	chmod 755 $(DEBIAN_DIR)

package:
	@echo "==> Building Debian package"
	dpkg-deb --build $(ROOTFS) $(APP_NAME)_$(VERSION)_amd64.deb
	@echo "==> Package created: $(APP_NAME)_$(VERSION)_amd64.deb"

clean:
	@echo "==> Cleaning up"
	rm -rf $(ROOTFS)
	rm -f $(APP_NAME)_*.deb
	rm -f rlv
