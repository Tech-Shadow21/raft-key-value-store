// This file exists only so `go build ./...` / `go test ./...` run from the
// repo root treat web/ as a separate module boundary and don't descend into
// it — web/ is a Next.js app, not Go code, but some npm packages (e.g.
// flatted) ship an incidental Go port inside node_modules that the Go
// toolchain would otherwise pick up.
module raftkv/web-placeholder

go 1.21
