import assert from "node:assert/strict";
import test from "node:test";

import {
  apiErrorMessage,
  audioSummary,
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

test("duration and file-size formatting", () => {
  assert.equal(formatDuration(0), "0:00");
  assert.equal(formatDuration(1421), "23:41");
  assert.equal(formatDuration(5690), "1:34:50");
  assert.equal(formatDuration(-1), "--:--");
  assert.equal(fileSize(1936423), "1.8 MiB");
  assert.equal(progressPercent(-1), 0);
  assert.equal(progressPercent(0.7459), 75);
  assert.equal(progressPercent(2), 100);
});

test("queue position and settings request", () => {
  assert.equal(queuePosition(["job-1", "job-3", "job-2"], "job-2"), 2);
  assert.equal(queuePosition(["job-1"], "missing"), -1);
  assert.deepEqual(settingsPayload({
    input_directory: "/input",
    output_directory: "/output",
    chapter_interval: "23:40",
  }), {
    input_directory: "/input",
    output_directory: "/output",
    chapter_interval: "23:40",
  });
});

test("chapter output durations follow the current checkbox selection", () => {
  const chapters = [
    { number: 1, start_seconds: 0 },
    { number: 2, start_seconds: 120 },
    { number: 3, start_seconds: 300 },
    { number: 4, start_seconds: 540 },
  ];
  assert.deepEqual([...chapterOutputDurations(chapters, 600, [1, 3], false)], [[1, 300], [3, 300]]);
  assert.deepEqual([...chapterOutputDurations(chapters, 600, [1, 2, 4], true)], [[1, 120], [2, 420]]);
});

test("audio summary supports selected and no-audio jobs", () => {
  assert.equal(audioSummary({ audio_selections: [{ track: 1, quality: "high" }, { track: 3, quality: "standard" }] }), "Track 1: 高音質, Track 3: 低音質");
  assert.equal(audioSummary({ audio_selections: [] }), "なし");
});

test("runtime labels distinguish pause and failure", () => {
  assert.equal(runtimeLabel(null), "待機中");
  assert.equal(runtimeLabel({ queue_paused: true }), "キュー一時停止");
  assert.equal(runtimeLabel({ running: true, current: {} }), "エンコード実行中");
  assert.equal(runtimeLabel({ running: true, current: { encoding_paused: true } }), "エンコード一時停止");
  assert.equal(runtimeLabel({ failure: { message: "failed" } }), "異常停止");
});

test("queue row state and delete boundary", () => {
  const job = { id: "job-1" };
  const running = { running: true, current: { job_id: "job-1", encoding_paused: false } };
  assert.equal(queueJobState(job, running), "実行中");
  assert.equal(queueJobState(job, { ...running, current: { ...running.current, encoding_paused: true } }), "一時停止");
  assert.equal(queueJobState(job, {
    failure: { message: "failed" },
    persistent_state: { job_id: "job-1" },
  }), "異常停止");
  assert.equal(queueJobState({ id: "job-2" }, running), "待機");
  assert.equal(canDeleteJob(job, running), false);
  assert.equal(canDeleteJob({ id: "job-2" }, running), true);
});

test("path, arrays, and structured errors", () => {
  assert.equal(outputName("/Volumes/Movies/作品 #01.mkv"), "作品 #01.mkv");
  assert.deepEqual(normalizeArray(null), []);
  assert.deepEqual(normalizeArray([1, 2]), [1, 2]);
  assert.equal(apiErrorMessage({ payload: { error: { message: "構造化エラー" } } }), "構造化エラー");
});
