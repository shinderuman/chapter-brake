import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const appSource = await readFile(new URL("./app.js", import.meta.url), "utf8");
const indexSource = await readFile(new URL("./index.html", import.meta.url), "utf8");

test("rendered markup does not use CSP-blocked inline styles", () => {
  assert.doesNotMatch(appSource, /\sstyle\s*=/i);
});

test("analysis busy state reports determinate progress", () => {
  assert.match(appSource, /<progress class="busy-progress"[^>]*max="100" value="0">0%<\/progress>/);
  assert.match(appSource, /"入力を解析中…", true/);
  assert.match(appSource, /analysis-progress\/\$\{encodeURIComponent\(id\)\}/);
  assert.match(appSource, /updateBusyProgress\(payload\.progress\)/);
  assert.match(appSource, /const draftRequest = api\("\/drafts"[\s\S]*?monitorAnalysisProgress/);
  assert.match(appSource, /requestController\?\.abort\(\)/);
});

test("completed workflow steps are direct back-navigation controls", () => {
  assert.match(appSource, /data-action="go-workflow-step" data-step="\$\{index\}"/);
  assert.match(appSource, /const enabled = index <= current && !skipped/);
  assert.match(appSource, /case "go-workflow-step": await goToWorkflowStep/);
  assert.match(appSource, /if \(index === 0\) \{\s*state\.draft = null;\s*await openFiles/);
});

test("chapter checkbox changes refresh output totals immediately", () => {
  assert.match(appSource, /function refreshChapterOutputDurations\(form\)/);
  assert.match(appSource, /chapterOutputDurations\(/);
  assert.match(appSource, /output\.textContent = durations\.has/);
  assert.match(appSource, /input\[name="exclude_final"\][\s\S]*?refreshChapterOutputDurations/);
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
