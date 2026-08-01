import { createReadStream } from "node:fs";
import { stat } from "node:fs/promises";
import http from "node:http";
import { extname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(fileURLToPath(new URL("../../", import.meta.url)));
const webRoot = join(repoRoot, "web");
const basePath = "/apps/chapter-brake";
const port = Number(process.env.PORT || 4173);

const clients = new Set();
let draft = null;
let sequence = 4;
let settings = {
  version: 4,
  input_directory: "/Volumes/2TB HDD/Images/日本語 & 記号",
  output_directory: "/Volumes/2TB HDD/Movies",
  chapter_interval: "23:40",
};
let runtime = {
  running: true,
  queue_paused: false,
  pause_after_current: false,
  current: {
    job_id: "job-1",
    stage: "handbrake",
    progress: 0.37,
    eta_seconds: 896,
    duration_seconds: 1421,
    encoding_paused: false,
    log_path: "/tmp/chapter-brake-stage4-job-1.log",
  },
  queue_eta_seconds: 3012,
  persistent_state: { version: 1, status: "running", job_id: "job-1" },
};
let queue = {
  version: 1,
  jobs: [
    mockJob("job-1", "作品名 #01.mkv", 1, 6, 1421),
    mockJob("job-2", "作品名 #02.mkv", 7, 12, 1420),
    mockJob("job-3", "作品名 #03.mkv", 13, 18, 1421),
  ],
};

function mockJob(id, name, start, end, duration) {
  return {
    id,
    input: "/Volumes/2TB HDD/Images/日本語 & 記号/作品 第1巻_t00.mkv",
    output: `/Volumes/2TB HDD/Movies/作品名/${name}`,
    temporary_output: `/Volumes/2TB HDD/Movies/作品名/.${id}.tmp.mkv`,
    chapter_start: start,
    chapter_end: end,
    duration_seconds: duration,
    preset: "My MKV 1080p",
    container: "mkv",
    audio_tracks: [1],
    subtitles: [1],
    overwrite_approved: true,
  };
}

function baseDraft() {
  const starts = [0, 154, 659, 1326, 1416, 1542, 1640, 2189, 2779, 2837, 2843, 2898, 2988, 3754, 4168, 4258, 4264, 4338, 4428, 4996, 5086, 5270, 5638];
  const duration = 5685;
  return {
    id: "draft-000001",
    input: "/Volumes/2TB HDD/Images/日本語 & 記号/作品 第1巻_t00.mkv",
    duration_seconds: duration,
    chapters: starts.map((start, index) => ({
      number: index + 1,
      start_seconds: start,
      duration_seconds: (starts[index + 1] ?? duration) - start,
      output_duration_seconds: [1, 6, 12, 18].includes(index + 1) ? [1421, 1420, 1421, 1423][[1, 6, 12, 18].indexOf(index + 1)] : null,
      title: `Chapter ${index + 1}`,
    })),
    audio_tracks: [
      { number: 1, language: "日本語", name: "Main", codec: "DTS-HD MA", channels: 6, sample_rate: 48000 },
      { number: 2, language: "日本語", name: "Commentary", codec: "AC3", channels: 2, sample_rate: 48000 },
    ],
    subtitle_tracks: [
      { number: 1, language: "日本語", name: "日本語字幕", format: "PGS" },
      { number: 2, language: "English", name: "English", format: "PGS" },
    ],
    base: "作品名",
    start_index: 1,
    chapter_interval: "23:40",
    selected_chapters: [1, 6, 12, 18],
    selected_audio: [1],
    selected_subtitles: [],
    auto_chapters: true,
    tail_merged: true,
    exclude_final: false,
  };
}

function json(response, status, payload) {
  response.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    "Cache-Control": "no-store",
  });
  response.end(JSON.stringify(payload));
}

function snapshot() {
  return { queue, runtime };
}

function emitSnapshot() {
  const body = `event: snapshot\ndata: ${JSON.stringify(snapshot())}\n\n`;
  for (const client of clients) client.write(body);
}

async function readBody(request) {
  const chunks = [];
  for await (const chunk of request) chunks.push(chunk);
  return chunks.length ? JSON.parse(Buffer.concat(chunks).toString("utf8")) : {};
}

function updateDraft(path, body) {
  if (path.endsWith("/preset")) {
    draft.preset = {
      name: body.name,
      summary: body.source === "standard" ? "HandBrake標準プリセット" : "My Presets",
      container: body.name.includes("MP4") ? "mp4" : "mkv",
      source: body.source,
    };
  } else if (path.endsWith("/naming")) {
    draft.base = body.base;
    draft.start_index = body.start_index;
  } else if (path.endsWith("/chapters")) {
    draft.chapter_interval = body.interval;
    draft.selected_chapters = body.approximate ? [1, 6, 12, 18] : body.selected_chapters;
    draft.auto_chapters = body.approximate;
    draft.tail_merged = body.approximate;
    draft.exclude_final = body.exclude_final;
  } else if (path.endsWith("/audio")) {
    draft.selected_audio = body.tracks;
  } else if (path.endsWith("/subtitles")) {
    draft.selected_subtitles = body.tracks;
  }
  delete draft.preview;
}

function buildPreview() {
  const starts = draft.selected_chapters;
  const finalChapter = draft.exclude_final ? draft.chapters.length - 1 : draft.chapters.length;
  const jobs = starts.map((start, index) => {
    const end = (starts[index + 1] ?? finalChapter + 1) - 1;
    const job = mockJob(`preview-${index + 1}`, `${draft.base} #${String(draft.start_index + index).padStart(2, "0")}.${draft.preset.container}`, start, end, [1421, 1420, 1421, 1423][index] ?? 800);
    job.preset = draft.preset.name;
    job.container = draft.preset.container;
    job.audio_tracks = draft.selected_audio;
    job.subtitles = draft.selected_subtitles;
    return job;
  });
  draft.preview = {
    jobs,
    collisions: [`/Volumes/2TB HDD/Movies/${draft.base}/${draft.base} #01.${draft.preset.container}`],
  };
}

async function serveAPI(request, response, path) {
  if (request.method === "POST" && path === "/api/test/failure") {
    runtime.running = false;
    runtime.queue_paused = true;
    runtime.current = null;
    runtime.failure = {
      stage: "verify",
      message: "テスト用の検証エラーです",
      log_path: "/tmp/chapter-brake-stage4-job-1.log",
    };
    runtime.persistent_state = { version: 1, status: "failed", job_id: "job-1" };
    emitSnapshot();
    json(response, 200, runtime);
    return;
  }
  if (request.method === "POST" && path === "/api/test/recover") {
    delete runtime.failure;
    runtime.queue_paused = true;
    runtime.persistent_state = { version: 1, status: "paused", job_id: "job-1" };
    emitSnapshot();
    json(response, 200, runtime);
    return;
  }
  if (request.method === "GET" && path === "/api/status") {
    json(response, 200, { ready: true, initial_directory: settings.input_directory, queue: runtime });
    return;
  }
  if (request.method === "GET" && path === "/api/settings") {
    json(response, 200, settings);
    return;
  }
  if (request.method === "PUT" && path === "/api/settings") {
    settings = { version: 4, ...await readBody(request) };
    json(response, 200, settings);
    return;
  }
  if (request.method === "GET" && path === "/api/files") {
    json(response, 200, {
      directory: "/Volumes/2TB HDD/Images/日本語 & 記号",
      entries: [
        { Name: "サブ フォルダ", Path: "/Volumes/2TB HDD/Images/日本語 & 記号/サブ フォルダ", IsDir: true, Size: 0 },
        { Name: "作品 第1巻_t00.mkv", Path: "/Volumes/2TB HDD/Images/日本語 & 記号/作品 第1巻_t00.mkv", IsDir: false, Size: 1936423000 },
        { Name: "作品 #2 & 特典_t00.mkv", Path: "/Volumes/2TB HDD/Images/日本語 & 記号/作品 #2 & 特典_t00.mkv", IsDir: false, Size: 1736423000 },
      ],
    });
    return;
  }
  if (request.method === "GET" && path === "/api/presets") {
    const standard = new URL(request.url, "http://localhost").searchParams.get("source") === "standard";
    json(response, 200, standard ? {
      source: "standard",
      presets: [{ Category: "General", Name: "Fast 1080p30" }, { Category: "Matroska", Name: "H.265 MKV 1080p30" }],
    } : {
      source: "curated",
      presets: [
        { name: "My MKV 1080p", summary: "1080p / MKV", container: "mkv" },
        { name: "My MP4 1080p", summary: "1080p / MP4", container: "mp4" },
        { name: "My MKV 480p Crop", summary: "480p / MKV / crop", container: "mkv" },
        { name: "My MKV 480p No Crop", summary: "480p / MKV / cropなし", container: "mkv" },
      ],
    });
    return;
  }
  if (request.method === "POST" && path === "/api/drafts") {
    draft = baseDraft();
    json(response, 201, draft);
    return;
  }
  if (path.startsWith("/api/drafts/")) {
    const body = request.method === "GET" ? {} : await readBody(request);
    if (!draft) {
      json(response, 404, { error: { code: "draft_not_found", stage: "draft", message: "ドラフトがありません" } });
      return;
    }
    if (request.method === "PUT") updateDraft(path, body);
    if (request.method === "POST" && path.endsWith("/preview")) buildPreview();
    if (request.method === "POST" && path.endsWith("/queue")) {
      if (draft.preview.collisions.length && !body.overwrite_approved) {
        json(response, 409, { error: { code: "queue_add_failed", stage: "queue-add", message: "既存出力の上書き承認が必要です" } });
        return;
      }
      queue.jobs.push(...draft.preview.jobs.map(job => ({ ...job, id: `job-${sequence++}` })));
      draft = null;
      emitSnapshot();
      json(response, 201, { added: 4, queue });
      return;
    }
    json(response, 200, draft);
    return;
  }
  if (request.method === "GET" && path === "/api/queue") {
    json(response, 200, snapshot());
    return;
  }
  if (request.method === "POST" && path === "/api/queue/start") {
    runtime.running = queue.jobs.length > 0;
    runtime.queue_paused = false;
    runtime.current = runtime.running ? {
      job_id: queue.jobs[0].id,
      stage: "handbrake",
      progress: 0.12,
      eta_seconds: 1800,
      duration_seconds: queue.jobs[0].duration_seconds,
      encoding_paused: false,
      log_path: "/tmp/chapter-brake-stage4-job-1.log",
    } : null;
  } else if (request.method === "POST" && path === "/api/queue/encoding/pause") {
    runtime.current.encoding_paused = true;
  } else if (request.method === "POST" && path === "/api/queue/encoding/resume") {
    runtime.current.encoding_paused = false;
  } else if (request.method === "PUT" && path === "/api/queue/pause-after-current") {
    runtime.pause_after_current = (await readBody(request)).enabled;
  } else if (request.method === "POST" && path === "/api/queue/abort") {
    runtime.running = false;
    runtime.queue_paused = true;
    runtime.current = null;
  } else if (request.method === "POST" && path.startsWith("/api/alerts/")) {
    delete runtime.failure;
  } else if (path.startsWith("/api/queue/")) {
    const id = decodeURIComponent(path.split("/")[3]);
    const index = queue.jobs.findIndex(job => job.id === id);
    if (request.method === "DELETE") {
      queue.jobs.splice(index, 1);
      emitSnapshot();
      response.writeHead(204);
      response.end();
      return;
    }
    if (request.method === "POST" && path.endsWith("/move")) {
      const body = await readBody(request);
      const destination = body.position ?? (body.direction === "up" ? index - 1 : index + 1);
      const [job] = queue.jobs.splice(index, 1);
      queue.jobs.splice(destination, 0, job);
    }
  } else {
    json(response, 404, { error: { code: "not_found", stage: "route", message: "APIがありません" } });
    return;
  }
  emitSnapshot();
  json(response, 200, runtime);
}

function serveEvents(request, response) {
  response.writeHead(200, {
    "Content-Type": "text/event-stream",
    "Cache-Control": "no-store",
    Connection: "keep-alive",
  });
  clients.add(response);
  response.write(`event: snapshot\ndata: ${JSON.stringify(snapshot())}\n\n`);
  response.write(`event: log\ndata: ${JSON.stringify({ path: "/tmp/chapter-brake-stage4-job-1.log", offset: 0, text: "Encoding: task 1 of 1, 37.00 %\\n" })}\n\n`);
  request.on("close", () => clients.delete(response));
}

async function serveStatic(response, path) {
  const relative = path === "/" || path === "" ? "index.html" : path.slice(1);
  const file = resolve(webRoot, relative);
  if (!file.startsWith(`${webRoot}/`)) {
    response.writeHead(403);
    response.end();
    return;
  }
  try {
    const info = await stat(file);
    if (!info.isFile()) throw new Error("not a file");
    const contentTypes = { ".html": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".mjs": "text/javascript; charset=utf-8" };
    response.writeHead(200, {
      "Content-Type": contentTypes[extname(file)] || "application/octet-stream",
      "Content-Security-Policy": "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'",
      "Cache-Control": "no-store",
    });
    createReadStream(file).pipe(response);
  } catch {
    response.writeHead(404);
    response.end("not found");
  }
}

const server = http.createServer(async (request, response) => {
  const url = new URL(request.url, `http://${request.headers.host}`);
  if (!url.pathname.startsWith(basePath)) {
    response.writeHead(302, { Location: `${basePath}/` });
    response.end();
    return;
  }
  const path = url.pathname.slice(basePath.length) || "/";
  if (path === "/api/events") {
    serveEvents(request, response);
    return;
  }
  if (path.startsWith("/api/")) {
    await serveAPI(request, response, path);
    return;
  }
  await serveStatic(response, path);
});

server.listen(port, "127.0.0.1", () => {
  process.stdout.write(`ChapterBrake Stage 4 UI fixture: http://127.0.0.1:${port}${basePath}/\n`);
});
