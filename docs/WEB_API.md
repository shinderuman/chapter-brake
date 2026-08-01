# ChapterBrake Web Backend API

## 1. Scope

This document defines the ChapterBrake-specific backend contract used through
Local Web App Server. The generic server removes `/apps/<id>` and forwards the
remaining `/api/...` path to ChapterBrake over the Unix socket named by
`LOCAL_WEB_SOCKET`.

The API expresses ChapterBrake operations only. It does not accept external
command names, HandBrakeCLI arguments, or per-job arbitrary output paths.

## 2. Process and lifecycle

- `LOCAL_WEB_SOCKET` is required. ChapterBrake has no TUI execution mode.
- The existing ChapterBrake advisory lock is acquired before reading or
  changing the queue, so duplicate Web backends cannot run together.
- The socket is created with mode `0600` and removed at shutdown.
- `SIGTERM`, `SIGINT`, and `SIGHUP` stop accepting HTTP requests, resume a
  paused HandBrake process if necessary, finish the current job, pause before
  the next job, and then exit.
- Immediate abort is available only through its explicit API operation.

## 3. JSON rules

- Request objects are typed and reject unknown fields.
- Request bodies are limited to 1 MiB and must contain one JSON value.
- Arrays such as audio tracks and subtitles must be JSON arrays, including
  when empty.
- Invalid handwritten `queue.json`, `settings.json`, and `state.json` remain
  fail-closed and are never repaired by the API.

Errors use:

```json
{
  "error": {
    "code": "queue_abort_failed",
    "stage": "handbrake",
    "message": "job job-1 failed during handbrake: ...",
    "log_path": "/Users/example/Library/Logs/ChapterBrake/job-....log"
  }
}
```

`code` is stable for client routing. `stage` describes the workflow or runner
stage. `message` is suitable for user display. `log_path` appears when a
related job log exists.

## 4. Endpoints

### Application and input

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/status` | Initial directory, readiness, queue runtime state |
| `GET` | `/api/settings` | Current persisted ChapterBrake settings |
| `PUT` | `/api/settings` | Validate and atomically persist settings |
| `GET` | `/api/files?directory=<absolute>` | Directories and MKV files sorted by name |
| `GET` | `/api/presets` | GUI-exported My Presets equivalent |
| `GET` | `/api/presets?source=standard` | HandBrake standard preset catalog |

`GET /api/files` uses the configured or command-line input directory when the
query is omitted. It lists only directories and regular `.mkv` files and does
not pre-scan media.

Settings update:

```json
{
  "input_directory":"/Volumes/2TB HDD/Images",
  "output_directory":"/Volumes/2TB HDD/Movies",
  "chapter_interval":"23:40"
}
```

Input and output paths must be absolute. The input directory must exist. The
output directory may be absent and is created when a queued job starts.

### Draft workflow

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/api/drafts` | Analyze an absolute MKV input and create an in-memory draft |
| `GET` | `/api/analysis-progress/{id}` | Current HandBrake scan progress; completed values remain briefly available so the client can stop polling without a 404 race |
| `GET` | `/api/drafts/{id}` | Current typed draft |
| `DELETE` | `/api/drafts/{id}` | Discard a draft |
| `PUT` | `/api/drafts/{id}/preset` | Select curated/exported or standard preset |
| `PUT` | `/api/drafts/{id}/naming` | Set output base name and starting number |
| `PUT` | `/api/drafts/{id}/chapters` | Set or recalculate chapter starts |
| `PUT` | `/api/drafts/{id}/audio` | Select input audio tracks |
| `PUT` | `/api/drafts/{id}/subtitles` | Select soft subtitle tracks |
| `POST` | `/api/drafts/{id}/preview` | Validate and produce immutable queue jobs |
| `POST` | `/api/drafts/{id}/queue` | Add the confirmed preview with overwrite approval |

Create:

```json
{"input":"/Volumes/2TB HDD/Images/作品 第1巻_t00.mkv","analysis_id":"browser-generated-uuid"}
```

When `analysis_id` is present, the UI polls the progress endpoint while the
draft request is running. Its `progress` value is the HandBrake `SCANNING`
fraction from `0` through `1`; it is not an estimated or repeating indicator.
The progress entry is removed when the draft request finishes or fails.

Preset:

```json
{"name":"MKV Presets","source":"curated"}
```

`source` is `curated` or `standard`. The browser never provides an import-file
path or HandBrakeCLI arguments.

Naming:

```json
{"base":"作品 第1巻","start_index":1}
```

Only the base name and positive index are accepted. The output root comes from
`settings.json`, and the existing naming package creates final paths.

Chapters:

```json
{
  "interval":"23:40",
  "selected_chapters":[1,7,13,19],
  "exclude_final":false,
  "approximate":false
}
```

When `approximate` is true, the server ignores `selected_chapters` and runs the
existing nearest-chapter and tail-merge algorithm. Encoding jobs continue to
store chapter-number ranges only.

Audio and subtitles:

```json
{"selections":[{"track":1,"quality":"high"},{"track":3,"quality":"standard"}]}
```

```json
{"tracks":[1,2]}
```

The first body is for audio. Each available track accepts `high`, `standard`,
or omission from `selections`; an empty array means no audio. The second body is
for subtitles. MP4 subtitle selection is rejected, and subtitle burn-in remains disabled.

Queue add:

```json
{"overwrite_approved":true}
```

A preview is required. Existing output collisions require explicit approval.
Successful addition starts the queue immediately unless it is paused, then
removes the in-memory draft.

### Queue and runtime

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/queue` | Persistent jobs plus live runtime state |
| `GET` | `/api/queue/{id}` | Job detail plus live runtime state |
| `DELETE` | `/api/queue/{id}` | Delete a waiting job |
| `POST` | `/api/queue/{id}/move` | Move a waiting job by direction or absolute position |
| `POST` | `/api/queue/start` | Manually start or resume a paused queue |
| `POST` | `/api/queue/encoding/pause` | Send `SIGSTOP` to active HandBrake group |
| `POST` | `/api/queue/encoding/resume` | Send `SIGCONT` to active HandBrake group |
| `PUT` | `/api/queue/pause-after-current` | Reserve or cancel a job-boundary pause |
| `POST` | `/api/queue/abort` | Immediately cancel and pause the queue |
| `POST` | `/api/alerts/{id}/dismiss` | Dismiss the matching persistent failure |

Move:

```json
{"direction":"up"}
```

`direction` is `up` or `down`. The persistent queue store rejects moving or
deleting the active job and rejects moving another job ahead of it.

SortableJS sends a zero-based queue position instead:

```json
{"position":3}
```

Exactly one of `direction` and `position` is accepted.

Pause after current:

```json
{"enabled":true}
```

Immediate abort waits for process termination and partial-output cleanup before
returning. The current job remains queue head and automatic execution stays
paused. A later `/api/queue/start` reruns it from the beginning.

## 5. Runtime state

The queue response contains a `runtime` object:

```json
{
  "running":true,
  "queue_paused":false,
  "pause_after_current":false,
  "current":{
    "job_id":"20260729T120000.000000000-000001-0001",
    "stage":"handbrake",
    "progress":0.42,
    "eta_seconds":391,
    "duration_seconds":1421,
    "encoding_paused":false,
    "log_path":"/Users/example/Library/Logs/ChapterBrake/job-....log"
  },
  "queue_eta_seconds":1750,
  "persistent_state":{"version":1,"status":"running"}
}
```

Transitions:

```text
idle --queue add/manual start--> running
running --SIGSTOP--> running + encoding_paused
running + encoding_paused --SIGCONT--> running
running --pause-after-current--> paused after successful current job
running --immediate abort--> process stop + cleanup --> paused, same head
running --failure--> failed + paused, same head
paused --manual start--> running from queue head
running --queue empty--> idle
running --backend termination--> finish current --> paused --> process exit
```

`state.json` remains the persistent authority for running/failure recovery.
Progress, ETA, native pause state, and in-memory drafts are intentionally not
persistent.

## 6. Server-Sent Events

`GET /api/events` emits:

- `snapshot`: queue plus runtime state, immediately and after changes.
- `log`: appended bytes from the active job log, with path and byte offset.
- comment heartbeat every 15 seconds.

Example:

```text
event: snapshot
data: {"queue":{"version":1,"jobs":[]},"runtime":{"running":false,...}}

event: log
data: {"path":"/.../job.log","offset":0,"text":"..."}
```

Each connection receives a complete initial snapshot. Disconnecting cancels
only that stream; reconnecting obtains current state and does not affect the
runner.

## 7. Known constraints

- Drafts are in memory and disappear when the backend exits. Persistent queue
  jobs and runner state do not.
- Log events are capped at 256 KiB per event and continue from the next offset.
- Only one ChapterBrake process and one encoding job are supported.
- The API is intended for the generic server on loopback or a trusted LAN and
  a trusted installed app. It does not implement authentication or TLS and
  must not be internet-exposed.
