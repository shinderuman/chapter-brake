# Web Backend Stage 3 Result

Date: 2026-07-29

## Decision

`GO`

ChapterBrake's existing application, queue, runner, media, preset, naming, and
external-tool boundaries can be exposed as a typed local HTTP API without
moving app-specific behavior into Local Web App Server and without changing
queue or encoding semantics.

Stage 4 may implement the HTML UI against `docs/WEB_API.md` after explicit user
approval.

## Product changes

- `LOCAL_WEB_SOCKET` selects the Web backend; absence retains the migration TUI.
- `internal/control` owns Web runtime state and delegates every queue/process
  operation to the existing runner.
- `internal/webapi` supplies typed JSON handlers, structured errors, draft
  workflow, queue control, SSE snapshots, and job-log append events.
- `bootstrap` shares one dependency assembly path between TUI and Web modes.
- Runner callbacks now include the opened job-log path; command generation and
  execution order are unchanged.

## Automated verification

The test suite covers:

- Complete draft workflow from real `app.Service` through queue creation.
- Curated/exported and standard preset routing.
- Naming, chapter approximation/manual selection, final exclusion, audio, and
  subtitles.
- Preview requirement and overwrite approval boundary.
- Queue list/detail/delete/move and active-job protection delegated to the
  existing queue store.
- Native encoding pause/resume, job-boundary pause, immediate abort, failure,
  and retry state.
- Strict JSON, unknown fields, invalid tracks, and structured error/log path.
- SSE initial snapshot, progress notifications, log append, disconnect, and
  reconnect.
- Unix socket permissions, HTTP serving, cancellation, graceful shutdown, and
  socket cleanup.

Successful commands:

```sh
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
go build ./...
git diff --check
```

The Stage 3 target packages were also checked with `go test -coverprofile` and
`go tool cover -func`. The combined `internal/control`, `internal/webapi`, and
`internal/runner` statement coverage is 75.9%, with no 0% function in those
packages.

## Real tools

Fixture:

```text
poc/artifacts/source-four-chapters.mkv
size: 1,936,423 bytes
SHA-256: be34d6d6be894985acdd8a77549acecf40b17d8f28dbbb47da081de54aa7977b
```

The existing real runner integration completed both MKV and MP4 through
HandBrakeCLI, ffprobe, mkvpropedit, and ffmpeg. Outputs and logs are retained at:

```text
/Volumes/CodexVault/chapter-brake-stage3-integration-20260729
```

Output checksums:

```text
838e0cdf4616733c216a31d107f81e08ad424296c5968722f90ab36884a18c6a  統合 Title #01.mkv
47d7075ad67878a1e69356a8c59cafba07733b192bcf5eab7c580dd27b647b94  統合 Title #01.mp4
```

The real HandBrake standard preset catalog and `Fast 1080p30` resolution test
also passed.

## Real Web backend

An actual `chapterbrake` binary ran with an isolated `HOME` and a short Unix
socket. The following succeeded:

- `GET /api/status`
- `GET /api/files`
- `GET /api/events` immediate snapshot
- real HandBrake analysis through `POST /api/drafts`
- curated MKV preset selection
- naming, `23:40` approximation, audio 1, no subtitles
- preview containing chapter range 1-4 and 12-second reference duration
- signal shutdown and socket removal

No queue job was added, so the real user queue and output directory were not
modified. This check found that an empty selected-subtitle response was encoded
as `null`; the API was corrected to return `[]`, covered by regression test,
rebuilt, and confirmed again through the real Unix socket.

## Known constraints

- Drafts are intentionally in memory and are lost on backend restart.
- Progress, ETA, and native pause state are not persisted; queue and failure
  recovery remain persisted in the existing JSON files.
- Job-log SSE emits at most 256 KiB per event and continues by byte offset.
- Only the loopback Local Web App Server contract is supported.
- No HTML app UI is included in Stage 3.
- TUI remains only as a migration frontend and is scheduled for deletion after
  Web acceptance, not permanent coexistence.
