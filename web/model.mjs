export function formatDuration(seconds) {
  if (!Number.isFinite(seconds) || seconds < 0) return "--:--";
  const rounded = Math.round(seconds);
  const hours = Math.floor(rounded / 3600);
  const minutes = Math.floor((rounded % 3600) / 60);
  const remaining = rounded % 60;
  if (hours > 0) return `${hours}:${String(minutes).padStart(2, "0")}:${String(remaining).padStart(2, "0")}`;
  return `${minutes}:${String(remaining).padStart(2, "0")}`;
}

export function fileSize(bytes) {
  if (!Number.isFinite(bytes) || bytes < 0) return "";
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KiB", "MiB", "GiB", "TiB"];
  let value = bytes / 1024;
  let unit = units[0];
  for (let index = 1; index < units.length && value >= 1024; index += 1) {
    value /= 1024;
    unit = units[index];
  }
  return `${value >= 10 ? value.toFixed(0) : value.toFixed(1)} ${unit}`;
}

export function progressPercent(value) {
  return Math.round(Math.max(0, Math.min(1, Number(value) || 0)) * 100);
}

export function runtimeLabel(runtime) {
  if (runtime?.failure) return "異常停止";
  if (runtime?.running && runtime.current?.encoding_paused) return "エンコード一時停止";
  if (runtime?.running) return "エンコード実行中";
  if (runtime?.queue_paused) return "キュー一時停止";
  return "待機中";
}

export function queueJobState(job, runtime) {
  if (runtime?.failure && runtime?.persistent_state?.job_id === job.id) {
    return "異常停止";
  }
  if (runtime?.running && runtime.current?.job_id === job.id) {
    return runtime.current.encoding_paused ? "一時停止" : "実行中";
  }
  return "待機";
}

export function outputName(path) {
  if (!path) return "";
  return path.split("/").filter(Boolean).at(-1) ?? path;
}

export function normalizeArray(value) {
  return Array.isArray(value) ? value : [];
}

export function apiErrorMessage(error) {
  if (error?.payload?.error?.message) return error.payload.error.message;
  return error?.message || "処理に失敗しました";
}

export function canDeleteJob(job, runtime) {
  return !(runtime?.running && runtime.current?.job_id === job.id);
}

export function queuePosition(jobIDs, id) {
  return normalizeArray(jobIDs).indexOf(id);
}

export function settingsPayload(values) {
  return {
    input_directory: String(values.input_directory ?? ""),
    output_directory: String(values.output_directory ?? ""),
    chapter_interval: String(values.chapter_interval ?? ""),
  };
}
