# GUI for Clash-Web

This is a **Web service version** modified from the open-source desktop client [GUI.for.Clash](https://github.com/GUI-for-Cores/GUI.for.Clash).

This project removes the original Wails desktop dependencies, converting the frontend into a pure Web application. The frontend assets are embedded into the Go (Gin) backend via `go:embed`, allowing lightweight deployment on local networks, servers, or NAS environments.

## Document

Original document: [how-to-use](https://gui-for-cores.github.io/guide/gfc/how-to-use)

## Build

### 1. Requirements
- **Node.js** & **pnpm** (for frontend)
- **Go** (for backend)

### 2. Steps
```bash
# Clone the repository
git clone https://github.com/yuexps/GUI.for.Clash.Web.git
cd GUI.for.Clash.Web

# 1. Build the frontend
cd frontend
pnpm install && pnpm build

# 2. Build the Go backend (frontend assets will be embedded automatically)
cd ..
go build -o GUI-for-Clash-Web main.go
```

