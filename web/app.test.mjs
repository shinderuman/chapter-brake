import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const appSource = await readFile(new URL("./app.js", import.meta.url), "utf8");
const indexSource = await readFile(new URL("./index.html", import.meta.url), "utf8");

test("rendered markup does not use CSP-blocked inline styles", () => {
  assert.doesNotMatch(appSource, /\sstyle\s*=/i);
});

test("analysis busy state includes an indeterminate progress indicator", () => {
  assert.match(appSource, /<progress class="busy-progress"[^>]*><\/progress>/);
  assert.match(appSource, /"入力を解析中…", true/);
});

test("continuous queue and log updates preserve interactive dialog nodes", () => {
  assert.match(appSource, /renderKey === queueRenderKey/);
  assert.match(appSource, /renderKey === jobDialogRenderKey/);
  assert.match(appSource, /logView\.textContent = logText/);
});

test("job detail uses native dialog form close behavior", () => {
  assert.match(indexSource, /<dialog id="job-dialog"[\s\S]*?<form method="dialog">/);
  assert.match(indexSource, /type="submit" value="close" aria-label="詳細を閉じる"/);
  assert.doesNotMatch(appSource, /aria-label="詳細を閉じる"/);
  assert.match(appSource, /if \(!\["naming-form"[\s\S]*?\.includes\(event\.target\.id\)\) return;\n  event\.preventDefault\(\);/);
});
