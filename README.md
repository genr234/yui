# Kiosk Platform Boilerplate

Minimal scaffold for the kiosk monorepo with a mock-first platform flow that is easy to test on macOS.

## Local testing on macOS

### Platform only

```sh
cd platform
npm install
npm run dev
```

Then open the Vite URL and use the overlay buttons. In dev mode, the SDK automatically uses a browser-side mock bridge.

### Controller placeholder

```sh
cd controller
go run .
```

This starts placeholder listeners on:

- `http://localhost:7070` — proxy stub
- `http://localhost:7071` — bridge stub
- `http://localhost:7072/demo.html` — static test page
