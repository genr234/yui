SHELL := /bin/zsh

ROOT_DIR := $(CURDIR)
PLATFORM_DIR := $(ROOT_DIR)/platform
CONTROLLER_DIR := $(ROOT_DIR)/controller
INSTALLER_DIR := $(ROOT_DIR)/installer
DIST_DIR := $(ROOT_DIR)/dist
CONTROLLER_ASSET := $(INSTALLER_DIR)/assets/controller.exe

.PHONY: help deps deps-platform dev dev-platform dev-controller \
	build build-local build-platform build-controller build-controller-local build-installer \
	fmt check clean

help:
	@echo "Available targets:"
	@echo "  make deps               Install platform dependencies"
	@echo "  make dev                Run controller + platform dev servers"
	@echo "  make dev-platform       Run the Vite dev server only"
	@echo "  make dev-controller     Run the Go controller only"
	@echo "  make build              Build platform + Windows controller + installer"
	@echo "  make build-local        Build the platform + a local controller binary"
	@echo "  make fmt                Format Go sources"
	@echo "  make check              Build-check the Go controller"
	@echo "  make clean              Remove generated artifacts"

deps: deps-platform

deps-platform:
	cd $(PLATFORM_DIR) && bun install

dev:
	@echo "Starting kiosk controller and platform dev server..."
	@trap 'kill 0' EXIT; \
		(cd $(CONTROLLER_DIR) && go run .) & \
		(cd $(PLATFORM_DIR) && bun run dev) & \
		wait

dev-platform:
	cd $(PLATFORM_DIR) && bun run dev

dev-controller:
	cd $(CONTROLLER_DIR) && go run .

build: build-platform build-controller build-installer

build-local: build-platform build-controller-local

build-platform:
	cd $(PLATFORM_DIR) && bun run build

build-controller:
	mkdir -p $(INSTALLER_DIR)/assets $(DIST_DIR)
	cd $(CONTROLLER_DIR) && GOOS=windows GOARCH=amd64 go build -o $(CONTROLLER_ASSET) .

build-controller-local:
	mkdir -p $(DIST_DIR)
	cd $(CONTROLLER_DIR) && go build -o $(DIST_DIR)/controller .

build-installer:
	mkdir -p $(DIST_DIR)
	cd $(INSTALLER_DIR) && GOOS=windows GOARCH=amd64 go build -o $(DIST_DIR)/installer.exe .

fmt:
	cd $(CONTROLLER_DIR) && gofmt -w .
	cd $(INSTALLER_DIR) && gofmt -w .

check:
	cd $(CONTROLLER_DIR) && go build ./...

clean:
	rm -rf $(DIST_DIR)
	rm -f $(CONTROLLER_ASSET)
	rm -f $(PLATFORM_DIR)/package-lock.json
