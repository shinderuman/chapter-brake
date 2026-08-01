import {
  apiErrorMessage,
  canDeleteJob,
  chapterOutputDurations,
  fileSize,
  formatDuration,
  normalizeArray,
  outputName,
  progressPercent,
  queueJobState,
  queuePosition,
  runtimeLabel,
  settingsPayload,
} from "./model.mjs";

const main = document.querySelector("#main");
const queueSummary = document.querySelector("#queue-summary");
const connectionStatus = document.querySelector("#connection-status");
const globalStatus = document.querySelector("#global-status");
const alertRegion = document.querySelector("#alert-region");
const toast = document.querySelector("#toast");
const confirmDialog = document.querySelector("#confirm-dialog");
const confirmTitle = document.querySelector("#confirm-title");
const confirmMessage = document.querySelector("#confirm-message");
const confirmButton = document.querySelector("#confirm-button");
const jobDialog = document.querySelector("#job-dialog");
const jobDialogTitle = document.querySelector("#job-dialog-title");
const jobDialogStatus = document.querySelector("#job-dialog-status");
const jobDialogContent = document.querySelector("#job-dialog-content");
const settingsDialog = document.querySelector("#settings-dialog");
const settingsForm = document.querySelector("#settings-form");
const queueCount = document.querySelector("#queue-count");

let queueSortable = null;
let queueDragging = false;
let queueRenderKey = "";
let jobDialogRenderKey = "";

const state = {
  view: "files",
  status: null,
  queue: { version: 1, jobs: [] },
  runtime: null,
  files: null,
  presets: [],
  presetSource: "curated",
  draft: null,
  selectedJobID: null,
  logs: new Map(),
  busy: false,
  lastInputDirectory: null,
};

class APIError extends Error {
  constructor(message, status, payload) {
    super(message);
    this.status = status;
    this.payload = payload;
  }
}

async function api(path, options = {}) {
  const request = { ...options, headers: { Accept: "application/json", ...(options.headers || {}) } };
  if (request.body && typeof request.body !== "string") {
    request.headers["Content-Type"] = "application/json";
    request.body = JSON.stringify(request.body);
  }
  const response = await fetch(`./api${path}`, request);
  const isJSON = response.headers.get("content-type")?.includes("application/json");
  const payload = isJSON ? await response.json() : null;
  if (!response.ok) {
    throw new APIError(payload?.error?.message || `HTTP ${response.status}`, response.status, payload);
  }
  return response.status === 204 ? null : payload;
}

function escapeHTML(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

function showToast(message, error = false) {
  toast.textContent = message;
  toast.classList.toggle("error", error);
  toast.classList.add("visible");
  clearTimeout(showToast.timer);
  showToast.timer = setTimeout(() => toast.classList.remove("visible"), 3500);
}

function setBusy(message = "処理中…", showProgress = false) {
  state.busy = true;
  main.innerHTML = `
    <div class="loading">
      <div class="loading-content">
        <div class="spinner"></div>
        <strong>${escapeHTML(message)}</strong>
        ${showProgress ? `<div class="busy-progress-row"><progress class="busy-progress" aria-label="解析進捗 0%" max="100" value="0">0%</progress><strong class="busy-progress-value">0%</strong></div>` : ""}
      </div>
    </div>
  `;
}

function updateBusyProgress(value) {
  const progress = main.querySelector(".busy-progress");
  const label = main.querySelector(".busy-progress-value");
  if (!progress || !label) return;
  const percent = progressPercent(value);
  progress.value = percent;
  progress.textContent = `${percent}%`;
  progress.setAttribute("aria-label", `解析進捗 ${percent}%`);
  label.textContent = `${percent}%`;
}

function monitorAnalysisProgress(id) {
  let stopped = false;
  let timer;
  let requestController;
  const poll = async () => {
    if (stopped) return;
    requestController = new AbortController();
    try {
      const payload = await api(`/analysis-progress/${encodeURIComponent(id)}`, { signal: requestController.signal });
      updateBusyProgress(payload.progress);
    } catch (error) {
      if (!stopped && error.name !== "AbortError" && error.status !== 404) showToast(apiErrorMessage(error), true);
    } finally {
      requestController = null;
      if (!stopped) timer = setTimeout(poll, 200);
    }
  };
  timer = setTimeout(poll, 100);
  return () => {
    stopped = true;
    clearTimeout(timer);
    requestController?.abort();
  };
}

async function perform(action, busyMessage, showProgress = false) {
  if (state.busy) return;
  if (busyMessage) setBusy(busyMessage, showProgress);
  try {
    return await action();
  } catch (error) {
    showToast(apiErrorMessage(error), true);
    throw error;
  } finally {
    state.busy = false;
  }
}

function render() {
  renderHeader();
  renderAlert();
  renderQueueSummary();
  switch (state.view) {
    case "files": renderFiles(); break;
    case "presets": renderPresets(); break;
    case "naming": renderNaming(); break;
    case "chapters": renderChapters(); break;
    case "audio": renderAudio(); break;
    case "subtitles": renderSubtitles(); break;
    case "preview": renderPreview(); break;
    default: renderFiles();
  }
  decorateWorkflow();
}

function renderHeader() {
  const label = runtimeLabel(state.runtime);
  const eta = state.runtime?.queue_eta_seconds > 0 ? ` · 全体ETA 約${formatDuration(state.runtime.queue_eta_seconds)}` : "";
  globalStatus.textContent = `${label}${eta}`;
}

function renderAlert() {
  const failure = state.runtime?.failure;
  if (!failure) {
    alertRegion.replaceChildren();
    return;
  }
  alertRegion.innerHTML = `
    <span><strong>異常停止</strong> · ${escapeHTML(failure.stage)} · ${escapeHTML(failure.message)}</span>
    ${state.runtime?.persistent_state?.job_id ? `<button class="button small secondary" data-action="dismiss-alert" data-id="${escapeHTML(state.runtime.persistent_state.job_id)}">確認済みにする</button>` : ""}
  `;
}

function renderQueueSummary() {
  if (queueDragging) return;
  const jobs = normalizeArray(state.queue?.jobs);
  queueCount.textContent = String(jobs.length);
  const runtime = state.runtime;
  const renderKey = JSON.stringify({
    jobs: jobs.map(job => ({
      id: job.id,
      output: job.output,
      chapter_start: job.chapter_start,
      chapter_end: job.chapter_end,
      duration_seconds: job.duration_seconds,
      state: queueJobState(job, runtime),
      current: runtime?.current?.job_id === job.id,
      deletable: canDeleteJob(job, runtime),
    })),
    label: runtimeLabel(runtime),
    running: Boolean(runtime?.running),
    queue_paused: Boolean(runtime?.queue_paused),
    pause_after_current: Boolean(runtime?.pause_after_current),
    stage: runtime?.current?.stage || "",
    encoding_paused: Boolean(runtime?.current?.encoding_paused),
  });
  if (renderKey === queueRenderKey) {
    updateQueueProgress(runtime);
    return;
  }
  queueRenderKey = renderKey;
  queueSortable?.destroy();
  queueSortable = null;
  if (jobs.length === 0) {
    queueSummary.innerHTML = `<div class="queue-summary-empty">キューは空です</div>`;
    return;
  }
  const queueControl = !runtime?.running
    ? `<button class="button small" data-action="start-queue">${runtime?.queue_paused ? "キューを再開" : "キューを開始"}</button>`
    : `<span class="status-chip ${runtime.current?.encoding_paused ? "paused" : "ready"}">${escapeHTML(runtimeLabel(runtime))}</span>`;
  queueSummary.innerHTML = `<div class="rail-controls">${queueControl}</div>` + jobs.map((job, index) => {
    const status = queueJobState(job, state.runtime);
    const current = state.runtime?.current?.job_id === job.id;
    const progress = current ? progressPercent(state.runtime.current.progress) : 0;
    const directControls = current ? currentControls(state.runtime, true) : waitingControls(job, state.runtime, true);
    return `
      <article class="queue-mini ${current ? "current not-draggable" : ""}" data-job-id="${escapeHTML(job.id)}">
        <div class="queue-mini-top"><span>${String(index + 1).padStart(2, "0")} · ${escapeHTML(status)}</span><span>約${formatDuration(job.duration_seconds)}</span></div>
        <button class="queue-mini-detail" data-action="show-job" data-id="${escapeHTML(job.id)}"><strong>${escapeHTML(outputName(job.output))}</strong><small>Chapter ${job.chapter_start}–${job.chapter_end}</small></button>
        ${current ? `<progress class="queue-progress" aria-label="進捗 ${progress}%" max="100" value="${progress}">${progress}%</progress>` : ""}
        <div class="queue-mini-actions">${!current ? `<button class="drag-handle" type="button" aria-label="ドラッグして並び替え">↕</button>` : ""}${directControls}</div>
      </article>
    `;
  }).join("");

  if (typeof window.Sortable !== "function") {
    showToast("キュー並び替えライブラリを読み込めません", true);
    return;
  }
  queueSortable = new window.Sortable(queueSummary, {
    handle: ".drag-handle",
    draggable: ".queue-mini:not(.not-draggable)",
    animation: 180,
    ghostClass: "queue-ghost",
    chosenClass: "queue-chosen",
    onStart: () => { queueDragging = true; },
    onEnd: async event => {
      queueDragging = false;
      const id = event.item.dataset.jobId;
      const position = queuePosition(
        [...queueSummary.querySelectorAll(".queue-mini")].map(item => item.dataset.jobId),
        id,
      );
      const previous = jobs.findIndex(job => job.id === id);
      if (previous === position) return;
      try {
        await queueAction(`/queue/${encodeURIComponent(id)}/move`, { method: "POST", body: { position } });
      } catch {
        await refreshQueue();
        queueRenderKey = "";
        renderQueueSummary();
      }
    },
  });
}

function updateQueueProgress(runtime) {
  if (!runtime?.current) return;
  const current = [...queueSummary.querySelectorAll(".queue-mini")]
    .find(item => item.dataset.jobId === runtime.current.job_id);
  const progress = current?.querySelector(".queue-progress");
  if (!progress) return;
  const percent = progressPercent(runtime.current.progress);
  progress.value = percent;
  progress.textContent = `${percent}%`;
  progress.setAttribute("aria-label", `進捗 ${percent}%`);
}

function pageHeader(step, title, copy = "", actions = "") {
  return `
    <header class="page-header">
      <div>
        <p class="eyebrow">${escapeHTML(step)}</p>
        <h1>${escapeHTML(title)}</h1>
        ${copy ? `<p>${escapeHTML(copy)}</p>` : ""}
      </div>
      ${actions}
    </header>
  `;
}

const workflowSteps = ["入力", "プリセット", "出力名", "チャプター", "音声", "字幕", "確認"];
const workflowViews = ["files", "presets", "naming", "chapters", "audio", "subtitles", "preview"];

function decorateWorkflow() {
  if (!main.firstElementChild || main.firstElementChild.classList.contains("flow-layout")) return;
  const current = workflowViews.indexOf(state.view);
  const content = document.createElement("div");
  content.className = "flow-content";
  while (main.firstChild) content.append(main.firstChild);
  const rail = document.createElement("nav");
  rail.className = "flow-steps";
  rail.setAttribute("aria-label", "ジョブ追加手順");
  rail.innerHTML = workflowSteps.map((label, index) => {
    const skipped = index === 5 && state.draft?.preset?.container === "mp4";
    const enabled = index <= current && !skipped;
    return `<button type="button" class="${index === current ? "active" : ""} ${index < current ? "done" : ""}" data-action="go-workflow-step" data-step="${index}" ${enabled ? "" : "disabled"} ${index === current ? 'aria-current="step"' : ""}><b>${String(index + 1).padStart(2, "0")}</b>${label}</button>`;
  }).join("");
  const layout = document.createElement("div");
  layout.className = "flow-layout";
  layout.append(rail, content);
  main.append(layout);
}

async function goToWorkflowStep(index) {
  const current = workflowViews.indexOf(state.view);
  if (!Number.isInteger(index) || index < 0 || index > current) return;
  if (index === 0) {
    state.draft = null;
    await openFiles(state.lastInputDirectory);
    return;
  }
  if (!state.draft || (index === 5 && state.draft.preset?.container === "mp4")) return;
  state.view = workflowViews[index];
  if (index === 1) {
    await loadPresetSource("curated");
    return;
  }
  render();
}

async function openFiles(directory) {
  await perform(async () => {
    const suffix = directory ? `?directory=${encodeURIComponent(directory)}` : "";
    state.files = await api(`/files${suffix}`);
    state.lastInputDirectory = state.files.directory;
    state.view = "files";
  }, "入力ファイルを読み込み中…");
  render();
}

function renderFiles() {
  const entries = normalizeArray(state.files?.entries);
  main.innerHTML = `
    ${pageHeader("STEP 01", "入力MKVを選択", "フォルダを開き、解析するMKVファイルを選んでください。")}
    <div class="path-bar">${escapeHTML(state.files?.directory || "")}</div>
    <div class="file-list">
      ${entries.map(entry => `
        <button class="list-card" data-action="${entry.IsDir ? "open-directory" : "analyze-file"}" data-path="${escapeHTML(entry.Path)}">
          <span><strong>${escapeHTML(entry.Name)}</strong><small>${entry.IsDir ? "フォルダ" : "MKVファイル"}</small></span>
          <span class="meta ${entry.IsDir ? "" : "file-size"}">${entry.IsDir ? "開く →" : fileSize(entry.Size)}</span>
        </button>
      `).join("") || `<div class="notice">このフォルダにMKVファイルはありません。</div>`}
    </div>
  `;
}

async function analyzeFile(path) {
  await perform(async () => {
    const analysisID = crypto.randomUUID();
    const draftRequest = api("/drafts", { method: "POST", body: { input: path, analysis_id: analysisID } });
    const stopProgress = monitorAnalysisProgress(analysisID);
    try {
      state.draft = await draftRequest;
      updateBusyProgress(1);
    } finally {
      stopProgress();
    }
    const payload = await api("/presets");
    state.presets = normalizeArray(payload.presets);
    state.presetSource = "curated";
    state.view = "presets";
  }, "入力を解析中…", true);
  render();
}

function renderPresets() {
  main.innerHTML = `
    ${pageHeader("STEP 02", state.presetSource === "curated" ? "My Presetsから選択" : "その他のプリセット", state.presetSource === "curated" ? "GUIエクスポート相当のプリセットを表示しています。" : "HandBrake標準プリセットから選択します。")}
    <div class="choice-list">
      ${state.presets.map(preset => `
        <button class="list-card" data-action="choose-preset" data-name="${escapeHTML(preset.name ?? preset.Name)}" data-source="${state.presetSource}">
          <span><strong>${escapeHTML(preset.name ?? preset.Name)}</strong><small>${escapeHTML(preset.summary ?? preset.Category ?? "")}</small></span>
          <span class="meta">${escapeHTML(preset.container ?? "")} →</span>
        </button>
      `).join("")}
    </div>
    <div class="button-row split">
      <button class="button ghost" data-action="back-files">入力選択へ戻る</button>
      ${state.presetSource === "curated" ? `<button class="button secondary" data-action="standard-presets">その他のプリセットから選ぶ</button>` : `<button class="button secondary" data-action="curated-presets">My Presetsへ戻る</button>`}
    </div>
  `;
}

async function loadPresetSource(source) {
  await perform(async () => {
    const payload = await api(`/presets${source === "standard" ? "?source=standard" : ""}`);
    state.presets = normalizeArray(payload.presets);
    state.presetSource = source;
  }, "プリセットを読み込み中…");
  render();
}

async function choosePreset(name, source) {
  await perform(async () => {
    state.draft = await api(`/drafts/${state.draft.id}/preset`, {
      method: "PUT", body: { name, source },
    });
    state.view = "naming";
  }, "プリセットを確認中…");
  render();
}

function renderNaming() {
  const outputRoot = state.draft?.preview?.jobs?.[0]?.output || "設定済みの出力先 / ベース名";
  main.innerHTML = `
    ${pageHeader("STEP 03", "出力名を決める", "入力したタイトルのフォルダを実行時に作成し、連番で保存します。")}
    <form id="naming-form" class="panel">
      <div class="form-grid">
        <div class="field full"><label for="base-name">出力ベース名</label><input id="base-name" name="base" type="text" value="${escapeHTML(state.draft.base)}" required autocomplete="off"></div>
        <div class="field"><label for="start-index">開始番号</label><input id="start-index" name="start_index" type="number" value="${state.draft.start_index}" min="1" required></div>
        <div class="field"><span class="field-label">形式</span><div class="status-chip ready">${escapeHTML(state.draft.preset?.container?.toUpperCase())}</div></div>
      </div>
      <div class="notice">出力例: ${escapeHTML(outputRoot)}</div>
      <div class="button-row split">
        <button class="button ghost" type="button" data-action="back-presets">プリセットへ戻る</button>
        <button class="button" type="submit">チャプター設定へ</button>
      </div>
    </form>
  `;
}

async function submitNaming(form) {
  const data = new FormData(form);
  await perform(async () => {
    state.draft = await api(`/drafts/${state.draft.id}/naming`, {
      method: "PUT",
      body: { base: data.get("base"), start_index: Number(data.get("start_index")) },
    });
    state.view = "chapters";
  }, "出力名を保存中…");
  render();
}

function renderChapters() {
  const selected = new Set(normalizeArray(state.draft.selected_chapters));
  main.innerHTML = `
    ${pageHeader("STEP 04", "チャプター開始位置", "チェックしたチャプターを各出力の開始位置として使います。")}
    <form id="chapters-form">
      <div class="panel">
        <div class="form-grid">
          <div class="field"><label for="chapter-interval">区切り時間（分:秒）</label><input id="chapter-interval" name="interval" type="text" value="${escapeHTML(state.draft.chapter_interval)}" pattern="[0-9]+:[0-5][0-9]" required></div>
          <div class="field"><span class="field-label">動画全体</span><div class="status-chip ready">${formatDuration(state.draft.duration_seconds)}</div></div>
        </div>
        <div class="button-row">
          <button class="button secondary" type="button" data-action="approximate-chapters">入力時間の近似値を選択</button>
          <button class="button secondary" type="button" data-action="clear-chapters">すべて外す</button>
        </div>
      </div>
      ${state.draft.tail_merged ? `<div id="tail-merged-notice" class="notice">最終チャプターは短い単体出力にせず、直前の出力へ結合されます。</div>` : ""}
      <label class="check-row">
        <input type="checkbox" name="exclude_final" ${state.draft.exclude_final ? "checked" : ""}>
        <span><strong>末尾の短いチャプターを除外</strong><small>2秒以下の場合は初期状態で有効になります。必要なチャプターなら解除できます。</small></span>
      </label>
      <div class="table-wrap">
        <table>
          <thead><tr><th>選択</th><th class="mono">番号</th><th class="mono">開始</th><th class="mono">単体</th><th class="mono">出力合計</th><th>タイトル</th></tr></thead>
          <tbody>
            ${state.draft.chapters.map(chapter => `
              <tr data-chapter="${chapter.number}" class="${selected.has(chapter.number) ? "selected-output" : ""}">
                <td><input aria-label="Chapter ${chapter.number}を出力開始位置にする" type="checkbox" name="chapter" value="${chapter.number}" ${selected.has(chapter.number) ? "checked" : ""}></td>
                <td class="mono">${String(chapter.number).padStart(3, "0")}</td>
                <td class="mono">${formatDuration(chapter.start_seconds)}</td>
                <td class="mono">${formatDuration(chapter.duration_seconds)}</td>
                <td class="mono" data-output-duration>${chapter.output_duration_seconds == null ? "—" : formatDuration(chapter.output_duration_seconds)}</td>
                <td>${escapeHTML(chapter.title || "")}</td>
              </tr>
            `).join("")}
          </tbody>
        </table>
      </div>
      <div class="button-row split">
        <button class="button ghost" type="button" data-action="back-naming">出力名へ戻る</button>
        <button class="button" type="submit">音声設定へ</button>
      </div>
    </form>
  `;
}

function chapterPayload(form, approximate = false) {
  const selected = [...form.querySelectorAll('input[name="chapter"]:checked')].map(input => Number(input.value));
  return {
    interval: form.elements.interval.value,
    selected_chapters: selected,
    exclude_final: form.elements.exclude_final.checked,
    approximate,
  };
}

function refreshChapterOutputDurations(form) {
  const finalChapter = state.draft.chapters.at(-1)?.number;
  const excludeFinal = form.elements.exclude_final.checked;
  if (excludeFinal) {
    const finalInput = form.querySelector(`input[name="chapter"][value="${finalChapter}"]`);
    if (finalInput) finalInput.checked = false;
  }
  const selected = [...form.querySelectorAll('input[name="chapter"]:checked')].map(input => Number(input.value));
  const durations = chapterOutputDurations(
    state.draft.chapters,
    state.draft.duration_seconds,
    selected,
    excludeFinal,
  );
  state.draft.selected_chapters = selected;
  state.draft.exclude_final = excludeFinal;
  state.draft.auto_chapters = false;
  state.draft.tail_merged = false;
  state.draft.preview = null;
  state.draft.chapters.forEach(chapter => {
    chapter.output_duration_seconds = durations.get(chapter.number) ?? null;
    const row = form.querySelector(`tr[data-chapter="${chapter.number}"]`);
    row?.classList.toggle("selected-output", durations.has(chapter.number));
    const output = row?.querySelector("[data-output-duration]");
    if (output) output.textContent = durations.has(chapter.number) ? formatDuration(durations.get(chapter.number)) : "—";
  });
  document.querySelector("#tail-merged-notice")?.remove();
}

async function updateChapters(form, approximate, next = false) {
  await perform(async () => {
    state.draft = await api(`/drafts/${state.draft.id}/chapters`, {
      method: "PUT", body: chapterPayload(form, approximate),
    });
    if (next) state.view = "audio";
  }, "チャプターを計算中…");
  render();
}

function renderAudio() {
  const selected = new Set(normalizeArray(state.draft.selected_audio));
  main.innerHTML = `
    ${pageHeader("STEP 05", "音声トラック", "選択した各入力音声から、高品質版と標準品質版の2音声を作成します。")}
    <form id="audio-form">
      <div class="check-list">
        ${state.draft.audio_tracks.map(track => `
          <label class="check-row">
            <input type="checkbox" name="track" value="${track.number}" ${selected.has(track.number) ? "checked" : ""}>
            <span><strong>Track ${track.number} · ${escapeHTML(track.language || "言語不明")}</strong><small>${escapeHTML([track.name, track.codec, track.channels ? `${track.channels}ch` : "", track.sample_rate ? `${track.sample_rate}Hz` : ""].filter(Boolean).join(" · "))}</small></span>
          </label>
        `).join("")}
      </div>
      <div class="notice">出力音声: 選択した各トラックにつき高品質 + 標準品質。数値ビットレートの指定は不要です。</div>
      <div class="button-row split">
        <button class="button ghost" type="button" data-action="back-chapters">チャプターへ戻る</button>
        <button class="button" type="submit">${state.draft.preset?.container === "mp4" ? "出力確認へ" : "字幕設定へ"}</button>
      </div>
    </form>
  `;
}

async function submitTracks(form, kind) {
  const tracks = [...form.querySelectorAll('input[name="track"]:checked')].map(input => Number(input.value));
  await perform(async () => {
    state.draft = await api(`/drafts/${state.draft.id}/${kind}`, { method: "PUT", body: { tracks } });
    if (kind === "audio" && state.draft.preset?.container !== "mp4") {
      state.view = "subtitles";
      return;
    }
    await buildPreview();
  }, kind === "audio" ? "音声設定を確認中…" : "字幕設定を確認中…");
  render();
}

function renderSubtitles() {
  const selected = new Set(normalizeArray(state.draft.selected_subtitles));
  main.innerHTML = `
    ${pageHeader("STEP 06", "字幕トラック", "字幕は映像へ焼き付けず、MKVのソフト字幕として格納します。")}
    <form id="subtitles-form">
      <div class="check-list">
        <label class="check-row">
          <input type="checkbox" id="no-subtitles" ${selected.size === 0 ? "checked" : ""}>
          <span><strong>字幕を入れない</strong><small>字幕トラックを選択しない状態です。</small></span>
        </label>
        ${state.draft.subtitle_tracks.map(track => `
          <label class="check-row">
            <input type="checkbox" name="track" value="${track.number}" ${selected.has(track.number) ? "checked" : ""}>
            <span><strong>Track ${track.number} · ${escapeHTML(track.language || "言語不明")}</strong><small>${escapeHTML([track.name, track.format].filter(Boolean).join(" · "))}</small></span>
          </label>
        `).join("")}
      </div>
      <div class="button-row split">
        <button class="button ghost" type="button" data-action="back-audio">音声へ戻る</button>
        <button class="button" type="submit">出力確認へ</button>
      </div>
    </form>
  `;
}

async function buildPreview() {
  state.draft = await api(`/drafts/${state.draft.id}/preview`, { method: "POST" });
  state.view = "preview";
}

function renderPreview() {
  const preview = state.draft.preview;
  main.innerHTML = `
    ${pageHeader("FINAL CHECK", "キューへ追加する内容", "出力名、チャプター範囲、音声、字幕を確認してください。")}
    <div class="metrics">
      <div class="metric"><span>出力数</span><strong>${preview.jobs.length}本</strong></div>
      <div class="metric"><span>形式</span><strong>${escapeHTML(state.draft.preset.container.toUpperCase())}</strong></div>
      <div class="metric"><span>字幕焼き付け</span><strong>なし</strong></div>
    </div>
    ${preview.excluded ? `<div class="notice warning">先頭のChapter ${preview.excluded.start}–${preview.excluded.end}は出力されません。</div>` : ""}
    ${preview.excluded_final ? `<div class="notice warning">末尾のChapter ${preview.excluded_final.start}は出力されません。</div>` : ""}
    <div class="table-wrap">
      <table>
        <thead><tr><th>出力</th><th>Chapter</th><th>参考時間</th><th>入力音声</th><th>字幕</th></tr></thead>
        <tbody>${preview.jobs.map(job => `
          <tr><td>${escapeHTML(outputName(job.output))}</td><td class="mono">${job.chapter_start}–${job.chapter_end}</td><td class="mono">${formatDuration(job.duration_seconds)}</td><td>${escapeHTML(job.audio_tracks.join(", "))}</td><td>${job.subtitles.length ? escapeHTML(job.subtitles.join(", ")) : "なし"}</td></tr>
        `).join("")}</tbody>
      </table>
    </div>
    ${preview.collisions.length ? `
      <div class="notice error"><strong>既存出力 ${preview.collisions.length}件</strong><br>${preview.collisions.map(path => escapeHTML(path)).join("<br>")}</div>
      <label class="check-row"><input id="overwrite-approved" type="checkbox"><span><strong>既存ファイルの上書きを承認する</strong><small>ジョブ実行時に追加確認せず置き換えます。</small></span></label>
    ` : ""}
    <div class="button-row split">
      <button class="button ghost" data-action="${state.draft.preset.container === "mp4" ? "back-audio" : "back-subtitles"}">設定へ戻る</button>
      <button class="button" data-action="add-queue">キューへ追加</button>
    </div>
  `;
}

async function addToQueue() {
  const overwriteApproved = document.querySelector("#overwrite-approved")?.checked ?? false;
  await perform(async () => {
    await api(`/drafts/${state.draft.id}/queue`, {
      method: "POST", body: { overwrite_approved: overwriteApproved },
    });
    state.draft = null;
    await refreshQueue();
    const suffix = state.lastInputDirectory ? `?directory=${encodeURIComponent(state.lastInputDirectory)}` : "";
    state.files = await api(`/files${suffix}`);
    state.lastInputDirectory = state.files.directory;
    state.view = "files";
  }, "キューへ追加中…");
  render();
  showToast("キューへ追加しました。続けて登録できます");
}

function renderJobDialog() {
  const job = normalizeArray(state.queue.jobs).find(item => item.id === state.selectedJobID);
  if (!job) {
    if (jobDialog.open) jobDialog.close();
    jobDialogRenderKey = "";
    return;
  }
  const runtime = state.runtime;
  const current = runtime?.current?.job_id === job.id;
  const failed = Boolean(runtime?.failure && runtime?.persistent_state?.job_id === job.id);
  const logPath = current ? runtime.current.log_path : failed ? runtime?.failure?.log_path : "";
  const logText = logPath ? state.logs.get(logPath) || "" : "";
  const renderKey = JSON.stringify({
    id: job.id,
    status: queueJobState(job, runtime),
    current,
    failed,
    logPath,
    stage: runtime?.current?.stage || "",
    encoding_paused: Boolean(runtime?.current?.encoding_paused),
    pause_after_current: Boolean(runtime?.pause_after_current),
    deletable: canDeleteJob(job, runtime),
  });
  if (jobDialog.open && renderKey === jobDialogRenderKey) {
    jobDialogStatus.textContent = `${queueJobState(job, runtime)} · 約${formatDuration(job.duration_seconds)}`;
    const logView = jobDialogContent.querySelector(".log-view");
    if (logView) logView.textContent = logText || "ログ更新を待っています…";
    return;
  }
  jobDialogRenderKey = renderKey;
  jobDialogTitle.textContent = outputName(job.output);
  jobDialogStatus.textContent = `${queueJobState(job, runtime)} · 約${formatDuration(job.duration_seconds)}`;
  jobDialogContent.innerHTML = `
    <div class="modal-body">
      <div class="form-grid">
        <div class="field full"><span class="field-label">入力</span><div class="path-bar">${escapeHTML(job.input)}</div></div>
        <div class="field full"><span class="field-label">出力</span><div class="path-bar">${escapeHTML(job.output)}</div></div>
      </div>
      <div class="metrics">
        <div class="metric"><span>Chapter</span><strong>${job.chapter_start}–${job.chapter_end}</strong></div>
        <div class="metric"><span>参考動画時間</span><strong>${formatDuration(job.duration_seconds)}</strong></div>
        <div class="metric"><span>プリセット</span><strong>${escapeHTML(job.preset)}</strong></div>
      </div>
      <p>入力音声: ${escapeHTML(job.audio_tracks.join(", "))} ／ 字幕: ${job.subtitles.length ? escapeHTML(job.subtitles.join(", ")) : "なし"}</p>
      ${current ? currentControls(runtime) : waitingControls(job, runtime)}
      ${logPath ? `<p class="field-label">ジョブログ: ${escapeHTML(logPath)}</p><pre class="log-view">${escapeHTML(logText || "ログ更新を待っています…")}</pre>` : ""}
    </div>
  `;
}

function openJobDialog(id) {
  state.selectedJobID = id;
  jobDialogRenderKey = "";
  renderJobDialog();
  if (!jobDialog.open) jobDialog.showModal();
}

function currentControls(runtime, compact = false) {
  const encoding = runtime.current?.stage === "handbrake";
  const css = compact ? "button secondary tiny" : "button secondary";
  return `
    <div class="${compact ? "compact-actions" : "button-row"}">
      ${encoding ? `<button type="button" class="${css}" data-action="${runtime.current.encoding_paused ? "resume-encoding" : "pause-encoding"}">${runtime.current.encoding_paused ? "再開" : "一時停止"}</button>` : ""}
      <button type="button" class="${css}" data-action="pause-after" data-enabled="${!runtime.pause_after_current}">${runtime.pause_after_current ? "ジョブ後停止を取消" : "ジョブ後に停止"}</button>
      <button type="button" class="button danger ${compact ? "tiny" : ""}" data-action="abort-queue">即時中断</button>
    </div>
  `;
}

function waitingControls(job, runtime, compact = false) {
  if (!canDeleteJob(job, runtime)) return "";
  return `
    <div class="${compact ? "compact-actions" : "button-row"}">
      <button type="button" class="button danger ${compact ? "tiny" : "small"}" data-action="delete-job" data-id="${escapeHTML(job.id)}">削除</button>
    </div>
  `;
}

async function refreshQueue() {
  const payload = await api("/queue");
  state.queue = payload.queue;
  state.runtime = payload.runtime;
}

async function queueAction(path, options = {}) {
  await perform(async () => {
    const runtime = await api(path, options);
    if (runtime) state.runtime = runtime;
    await refreshQueue();
  });
  render();
}

function askConfirmation(title, message, actionLabel = "実行") {
  return new Promise(resolve => {
    confirmTitle.textContent = title;
    confirmMessage.textContent = message;
    confirmButton.textContent = actionLabel;
    confirmDialog.addEventListener("close", () => resolve(confirmDialog.returnValue === "confirm"), { once: true });
    confirmDialog.showModal();
  });
}

async function deleteJob(id) {
  if (jobDialog.open) jobDialog.close();
  if (!await askConfirmation("キューから削除", "待機中のジョブを削除します。元に戻せません。", "削除")) return;
  await queueAction(`/queue/${encodeURIComponent(id)}`, { method: "DELETE" });
}

async function abortQueue() {
  if (!await askConfirmation("即時中断", "現在の外部プロセスを停止し、部分出力を削除します。ジョブはキュー先頭に残ります。", "中断")) return;
  await queueAction("/queue/abort", { method: "POST" });
}

async function openSettings() {
  try {
    const settings = await api("/settings");
    settingsForm.elements.input_directory.value = settings.input_directory;
    settingsForm.elements.output_directory.value = settings.output_directory;
    settingsForm.elements.chapter_interval.value = settings.chapter_interval;
    settingsDialog.showModal();
  } catch (error) {
    showToast(apiErrorMessage(error), true);
    throw error;
  }
}

async function saveSettings(form) {
  const data = new FormData(form);
  try {
    const settings = await api("/settings", {
      method: "PUT",
      body: settingsPayload({
        input_directory: data.get("input_directory"),
        output_directory: data.get("output_directory"),
        chapter_interval: data.get("chapter_interval"),
      }),
    });
    state.lastInputDirectory = settings.input_directory;
    if (state.view === "files") await openFiles(settings.input_directory);
    settingsDialog.close();
    showToast("設定を保存しました");
  } catch (error) {
    showToast(apiErrorMessage(error), true);
    throw error;
  }
}

function connectEvents() {
  const source = new EventSource("./api/events");
  source.addEventListener("open", () => {
    connectionStatus.textContent = "接続済み";
    connectionStatus.className = "connection online";
  });
  source.addEventListener("snapshot", event => {
    const payload = JSON.parse(event.data);
    if (payload.queue) state.queue = payload.queue;
    if (payload.runtime) state.runtime = payload.runtime;
    if (payload.error?.message) showToast(payload.error.message, true);
    renderHeader();
    renderAlert();
    renderQueueSummary();
    if (jobDialog.open) renderJobDialog();
  });
  source.addEventListener("log", event => {
    const payload = JSON.parse(event.data);
    const existing = state.logs.get(payload.path) || "";
    state.logs.set(payload.path, existing + payload.text);
    if (jobDialog.open) renderJobDialog();
  });
  source.onerror = () => {
    connectionStatus.textContent = "再接続中";
    connectionStatus.className = "connection offline";
  };
}

document.addEventListener("click", async event => {
  const target = event.target.closest("[data-action]");
  if (!target) return;
  const { action, path, name, source, id, direction, enabled, value } = target.dataset;
  try {
    switch (action) {
      case "reload": location.reload(); break;
      case "close-confirm": confirmDialog.close(value); break;
      case "new-job": state.draft = null; await openFiles(state.lastInputDirectory); break;
      case "go-workflow-step": await goToWorkflowStep(Number(target.dataset.step)); break;
      case "open-settings": await openSettings(); break;
      case "close-settings": settingsDialog.close(); break;
      case "open-directory": await openFiles(path); break;
      case "analyze-file": await analyzeFile(path); break;
      case "back-files": await openFiles(state.lastInputDirectory); break;
      case "standard-presets": await loadPresetSource("standard"); break;
      case "curated-presets": await loadPresetSource("curated"); break;
      case "choose-preset": await choosePreset(name, source); break;
      case "back-presets": state.view = "presets"; render(); break;
      case "back-naming": state.view = "naming"; render(); break;
      case "back-chapters": state.view = "chapters"; render(); break;
      case "back-audio": state.view = "audio"; render(); break;
      case "back-subtitles": state.view = "subtitles"; render(); break;
      case "approximate-chapters": await updateChapters(document.querySelector("#chapters-form"), true); break;
      case "clear-chapters":
        document.querySelectorAll('input[name="chapter"]').forEach(input => { input.checked = false; });
        break;
      case "add-queue": await addToQueue(); break;
      case "show-job": openJobDialog(id); break;
      case "start-queue": await queueAction("/queue/start", { method: "POST" }); break;
      case "pause-encoding": await queueAction("/queue/encoding/pause", { method: "POST" }); break;
      case "resume-encoding": await queueAction("/queue/encoding/resume", { method: "POST" }); break;
      case "pause-after": await queueAction("/queue/pause-after-current", { method: "PUT", body: { enabled: enabled === "true" } }); break;
      case "abort-queue": await abortQueue(); break;
      case "move-job": await queueAction(`/queue/${encodeURIComponent(id)}/move`, { method: "POST", body: { direction } }); break;
      case "delete-job": await deleteJob(id); break;
      case "dismiss-alert": await queueAction(`/alerts/${encodeURIComponent(id)}/dismiss`, { method: "POST" }); break;
    }
  } catch {
    render();
  }
});

document.addEventListener("submit", async event => {
  if (!["naming-form", "chapters-form", "audio-form", "subtitles-form", "settings-form"].includes(event.target.id)) return;
  event.preventDefault();
  try {
    if (event.target.id === "naming-form") await submitNaming(event.target);
    if (event.target.id === "chapters-form") await updateChapters(event.target, false, true);
    if (event.target.id === "audio-form") await submitTracks(event.target, "audio");
    if (event.target.id === "subtitles-form") await submitTracks(event.target, "subtitles");
    if (event.target.id === "settings-form") await saveSettings(event.target);
  } catch {
    render();
  }
});

document.addEventListener("keydown", event => {
  if (event.key !== "Enter" || event.isComposing || event.target.tagName !== "INPUT") return;
  const form = event.target.form;
  if (!form || !["naming-form", "chapters-form"].includes(form.id)) return;
  event.preventDefault();
  form.requestSubmit();
});

document.addEventListener("change", event => {
  if (event.target.id === "no-subtitles" && event.target.checked) {
    document.querySelectorAll('#subtitles-form input[name="track"]').forEach(input => { input.checked = false; });
  }
  if (event.target.matches('#subtitles-form input[name="track"]') && event.target.checked) {
    document.querySelector("#no-subtitles").checked = false;
  }
  if (event.target.matches('#chapters-form input[name="chapter"]')) {
    const last = state.draft.chapters.at(-1)?.number;
    if (Number(event.target.value) === last && event.target.checked) {
      document.querySelector('input[name="exclude_final"]').checked = false;
    }
    refreshChapterOutputDurations(event.target.form);
  }
  if (event.target.matches('#chapters-form input[name="exclude_final"]')) {
    refreshChapterOutputDurations(event.target.form);
  }
});

async function initialize() {
  setBusy("ChapterBrakeへ接続中…");
  try {
    const [status, queuePayload] = await Promise.all([api("/status"), api("/queue")]);
    state.status = status;
    state.queue = queuePayload.queue;
    state.runtime = queuePayload.runtime;
    state.lastInputDirectory = status.initial_directory;
    state.files = await api(`/files?directory=${encodeURIComponent(status.initial_directory)}`);
    state.busy = false;
    render();
    connectEvents();
  } catch (error) {
    state.busy = false;
    main.innerHTML = `
      ${pageHeader("CONNECTION ERROR", "ChapterBrakeへ接続できません", apiErrorMessage(error))}
      <button class="button" data-action="reload">再読み込み</button>
    `;
    connectionStatus.textContent = "接続失敗";
    connectionStatus.className = "connection offline";
  }
}

initialize();
