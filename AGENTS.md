# Agent Development Guide

A file for [guiding coding agents](https://agents.md/).

## Commands

- **Build:** `make build`
- **Test:** `make test`
- **Lint:** `make lint`
- **Format:** `make fmt`
- **Clean:** `make clean`

## Directory Structure

- `cmd/` — application entry points
- `internal/` — core packages (explore subdirectories for details)
- `docs/` — documentation

## Verification

After making changes, run these steps in order:

1. `make fmt` — format code
2. `make build` — ensure it compiles
3. `make test` — all tests pass
4. `make lint` — no lint errors
