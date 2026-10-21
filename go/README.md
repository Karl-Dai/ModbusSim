# Go/Wails development

Both desktop apps and the protocol core use Go. The module root is this `go/` directory; npm workspaces live at the repository root.

```sh
# From the repository root
npm ci --ignore-scripts
npm run dev:slave:go
npm run dev:master:go
npm run build:slave:go
npm run build:master:go
npm run bindings:slave:go
npm run bindings:master:go
npm run test:go
```

The wrappers reuse Wails v2.14.0 or bootstrap that pinned CLI with Go. macOS requires 13+ and Xcode Command Line Tools. Linux requires GTK3 and WebKitGTK 4.0; Windows uses WebView2. Release targets are macOS Universal, Linux x64, Windows x64 and ARM64.

Generated JavaScript and TypeScript bindings are checked in under both frontends' `wailsjs/` directories. Rebuild them after changing bound APIs. Vite embeds assets into `go/cmd/{slavegui,mastergui}/assets/dist`; compiled output is ignored. macOS standalone apps are copied to each entrypoint's `build/release` directory.

Run `go vet ./...` and `go test -race -count=1 ./...` from this directory. Native production compilation also uses the `desktop,production` tags. CI tests Linux, macOS and Windows and builds both apps for every release target.

Legacy v1 projects and CSV headers are imported into native Go data structures. New projects use PascalCase fields and `Version: 2`; keep the original backup because earlier Rust releases cannot read Go v2 exports. See [migration guidance](../docs/go-migration.md).

Go/Wails releases are installed manually from Releases, with SHA256SUMS and a machine-readable checksum manifest. The apps have no Tauri updater or analytics dependency.
