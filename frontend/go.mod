// This stub makes frontend/ (a Node module) a separate Go module boundary so
// `go test ./...` at the repo root never descends into node_modules, where
// some npm packages ship stray Go source files.
module github.com/michael-freling/claude-forge/frontend

go 1.25.0
