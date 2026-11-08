# AGENTS.md

Guidance for AI coding agents working on this repository. Read README.md for the API itself; this file lists the rules that are not obvious from the code.

## Layout

- `internal/pddikti/` — HTTP client to the PDDIKTI upstream, typed errors (`errors.go`).
- `internal/server/` — Gin router, error mapping (`writeError`), CORS, API key, per-IP rate limit.
- `main.go` — reads env config only. No handler logic here.

## Hard rules

1. **No real personal data in the repo.** Never put a real NIM, student name, or campus/student pair in tests, fixtures, examples, docs, or commit messages. Use fictitious data (e.g. NIM `23010000001`, `CONTOH MAHASISWA`, `Universitas Contoh`). Real data only enters through env vars (`PDDIKTI_TEST_NIM`, `PDDIKTI_TEST_KAMPUS`).
2. **Exact NIM match only.** `VerifyNIM` must never fall back to a partial, fuzzy, or first candidate. No exact match → `ErrNotFound`; several campuses → `*AmbiguousError`. Do not remove or weaken `TestVerifyNIM_PartialNIMIsNotFound` or the detail-NIM re-check.
3. **Tests never call the real PDDIKTI.** Unit tests use `httptest` (see `fakeUpstream`). Anything hitting the real API goes in a file with `//go:build live`.
4. **Respect the upstream quota** (60 requests/min per public IP). Do not write loops, scripts, or bulk lookups against PDDIKTI, and do not add features that enable bulk harvesting.
5. **Do not silently weaken protections**: rate limit, `TRUSTED_PROXIES` defaulting to none, constant-time API key check, minimum search query length. Changing a default needs a stated reason and a README update.

## Conventions

- New client failures get a sentinel error in `errors.go`, a case in `writeError`, a test in `TestErrorMapping`, and a row in the README "Format Error" table.
- Error responses keep the shape `{"status":"error","code":"<slug>","error":"<message>"}`; clients rely on `code`, so never rename an existing slug.
- Access decisions use `eligible`, not `verified`. Keep both fields in the verify response.
- New config is an env var read in `main.go`, added to `server.Config`, `.env.example`, and the README config table.
- User-facing messages are in Indonesian; code, identifiers, and comments in English.

## Before you finish

```bash
gofmt -l .            # must print nothing
go vet ./... && go vet -tags=live ./...
go test -race -cover ./...
```
