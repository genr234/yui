root_dir := justfile_directory()
platform_dir := root_dir + "/platform"
controller_dir := root_dir + "/controller"
installer_dir := root_dir + "/installer"
server_dir := root_dir + "/server"
dist_dir := root_dir + "/dist"
controller_asset := installer_dir + "/assets/controller.exe"
commit := `if [ -n "${YUI_COMMIT-}" ]; then printf "%s" "$YUI_COMMIT"; else git rev-parse HEAD 2>/dev/null || echo dev; fi`
server_image := `if [ -n "${YUI_SERVER_IMAGE-}" ]; then printf "%s" "$YUI_SERVER_IMAGE"; else printf "yui-server:%s" "${YUI_COMMIT:-dev}"; fi`

help:
  @echo "Available targets:"
  @echo "  just deps               Install platform dependencies"
  @echo "  just deps-all           Install platform + server dependencies"
  @echo "  just deps-server        Install Rails dependencies"
  @echo "  just dev                Run controller + platform + server dev servers"
  @echo "  just dev-platform       Run the Vite dev server only"
  @echo "  just dev-controller     Run the Go controller only"
  @echo "  just dev-server         Run the Rails server only"
  @echo "  just dev-inject Build Svelte and inject into Chrome via controller"
  @echo "  just dev-hot    Run Rails + Vite HMR + inject into Chrome via controller"
  @echo "  just build              Build platform + Rails server image + Windows controller + installer"
  @echo "  just build-kiosk        Build platform + Windows controller + installer"
  @echo "  just build-server       Build the Rails server container image"
  @echo "  just package            Build a clean deploy zip with checksum"
  @echo "  just build-local        Build the platform + a local controller binary"
  @echo "  just fmt                Format Go sources"
  @echo "  just check              Build-check Go/platform and run Rails CI"
  @echo "  just clean              Remove generated artifacts"

deps: deps-platform

deps-all: deps-platform deps-server

deps-platform:
  cd {{platform_dir}} && bun install

deps-server:
  cd {{server_dir}} && bundle install

dev:
  @trap 'kill 0' EXIT; (cd {{controller_dir}} && go run .) & (cd {{platform_dir}} && bun run dev) & (cd {{server_dir}} && bin/rails server) & wait

dev-platform:
  cd {{platform_dir}} && bun run dev

dev-controller:
  cd {{controller_dir}} && go run .

dev-server:
  cd {{server_dir}} && bin/rails server

dev-inject: build-platform
  cd {{controller_dir}} && go run .

dev-hot:
  @trap 'kill 0' EXIT; (cd {{server_dir}} && bin/rails server) & (cd {{platform_dir}} && bun run dev -- --host 127.0.0.1) & sleep 2; (cd {{controller_dir}} && YUI_PLATFORM_DEV_SERVER=http://127.0.0.1:5173 go run .) & wait

build: build-kiosk build-server

build-kiosk: build-platform build-controller build-installer

package: build-kiosk
  rm -rf {{dist_dir}}/release
  mkdir -p {{dist_dir}}/release
  cp {{dist_dir}}/installer.exe {{dist_dir}}/release/yui-kiosk-installer.exe
  cd {{dist_dir}}/release && shasum -a 256 yui-kiosk-installer.exe > SHA256SUMS.txt
  cd {{dist_dir}} && rm -f yui-kiosk-installer.zip && zip -qr yui-kiosk-installer.zip release

build-local: build-platform build-controller-local

build-platform:
  cd {{platform_dir}} && bun run build

build-server:
  docker build --pull -t {{server_image}} {{server_dir}}

build-controller:
  mkdir -p {{installer_dir}}/assets {{dist_dir}}
  cd {{controller_dir}} && GOOS=windows GOARCH=amd64 go build -ldflags "-X kiosk/controller/internal/version.Commit={{commit}}" -o {{controller_asset}} .

build-controller-local:
  mkdir -p {{dist_dir}}
  cd {{controller_dir}} && go build -ldflags "-X kiosk/controller/internal/version.Commit={{commit}}" -o {{dist_dir}}/controller .

build-installer:
  mkdir -p {{dist_dir}}
  cd {{installer_dir}} && GOOS=windows GOARCH=amd64 go build -o {{dist_dir}}/installer.exe .

fmt:
  cd {{controller_dir}} && gofmt -w .
  cd {{installer_dir}} && gofmt -w .
  cd {{server_dir}} && bin/rubocop -A

check:
  cd {{controller_dir}} && go build ./...
  cd {{installer_dir}} && go build ./...
  cd {{platform_dir}} && bun run build
  cd {{server_dir}} && bin/ci

clean:
  rm -rf {{dist_dir}}
  rm -f {{controller_asset}}
  rm -f {{platform_dir}}/package-lock.json
