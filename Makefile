build: build-platform build-controller build-installer

dev-platform:
	cd platform && npm run dev

dev-controller:
	cd controller && go run .

build-platform:
	cd platform && npm run build

build-controller:
	cd controller && GOOS=windows GOARCH=amd64 go build -o ../installer/assets/controller.exe .

build-installer:
	cd installer && GOOS=windows GOARCH=amd64 go build -o ../dist/installer.exe .
