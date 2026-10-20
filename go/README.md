# Go/Wails development

The slave GUI now uses the Go engine and the existing Vue slave workspace.
The master GUI remains on Tauri until M3. This branch is not a release build.

From the repository root:

```sh
npm run dev:slave:go
npm run build:slave:go
npm run bindings:slave:go
npm run test:go
npm test --workspace shared-frontend
```

The scripts reuse an installed Wails v2.14.0 CLI when available, and otherwise
bootstrap that pinned version through `go run`; no global Wails installation is required.
On macOS the wrapper supplies CGO and framework flags. The locally built app is
`go/cmd/slavegui/build/release/ModbusSlave.app`. The script keeps a standalone
copy outside `build/bin` so the development runner cannot delete its executable on exit. Other platforms need their normal Wails
native build dependencies and have not been exercised in this migration.

Generated JS/TypeScript bindings in `frontend/wailsjs` are checked-in source.
Regenerate them after changing bound DTOs or methods. Vite builds into
`go/cmd/slavegui/assets/dist`, which is embedded into the executable. Output assets
and binaries are ignored; the source `.gitkeep` allows binding generation and
Go tests before a first frontend build.

New projects use native PascalCase JSON fields with `Version: 2`; v1 projects
are imported once and cannot read back Go v2 exports in the Rust version.
Native CSV exports likewise use PascalCase fields; legacy CSV headers and
simulation keys are translated at import. The slave has no analytics or Tauri
updater; its Help menu opens Releases. The release workflows still target Rust
and need a separate migration before publishing a Go release.

See `../docs/brainstorm/2026-09-19-go-fullstack-rewrite-design.md` for milestones
and verification limits.
