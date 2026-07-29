const specialPath = "/Volumes/動画 HDD/記号 #1 & test.mkv";

const elements = {
  connection: document.querySelector("#connection"),
  backendStatus: document.querySelector("#backend-status"),
  queueCount: document.querySelector("#queue-count"),
  runState: document.querySelector("#run-state"),
  queue: document.querySelector("#queue"),
  reload: document.querySelector("#reload"),
  pathProof: document.querySelector("#path-proof"),
  errorPanel: document.querySelector("#error-panel"),
  errorMessage: document.querySelector("#error-message"),
};

const requestJSON = async (path, options = {}) => {
  const response = await fetch(path, options);
  if (!response.ok) {
    let message = `${response.status} ${response.statusText}`;
    try {
      const payload = await response.json();
      message = payload.detail || payload.error || message;
    } catch {
      // A proxy failure may not include JSON if the server is stopping.
    }
    throw new Error(message);
  }
  return response.json();
};

const setConnection = (state, message) => {
  elements.connection.className = `connection ${state}`;
  elements.connection.lastChild.textContent = ` ${message}`;
};

const renderSnapshot = (snapshot) => {
  const jobs = snapshot.queue?.jobs || [];
  elements.queueCount.textContent = String(jobs.length);
  elements.runState.textContent = snapshot.state?.status || "不明";
  elements.queue.replaceChildren();
  if (jobs.length === 0) {
    const empty = document.createElement("div");
    empty.className = "empty";
    empty.textContent = "キューは空です";
    elements.queue.append(empty);
    return;
  }
  jobs.forEach((job, index) => {
    const item = document.createElement("article");
    item.className = "job";
    const number = document.createElement("strong");
    number.textContent = `${index + 1}.`;
    const name = document.createElement("div");
    const title = document.createElement("strong");
    title.textContent = job.output?.split("/").pop() || job.id;
    const detail = document.createElement("small");
    detail.textContent = `chapter ${job.chapter_start}-${job.chapter_end}`;
    name.append(title, document.createElement("br"), detail);
    const duration = document.createElement("small");
    duration.textContent = job.duration_seconds ? `約${job.duration_seconds}秒` : "時間不明";
    item.append(number, name, duration);
    elements.queue.append(item);
  });
};

const loadSnapshot = async () => {
  const [status, queue, state, echo] = await Promise.all([
    requestJSON("api/status"),
    requestJSON("api/queue"),
    requestJSON("api/state"),
    requestJSON(`api/echo?path=${encodeURIComponent(specialPath)}`),
  ]);
  elements.backendStatus.textContent = status.ready ? "準備完了" : "未準備";
  elements.pathProof.textContent = echo.path;
  if (echo.path !== specialPath) {
    throw new Error("特殊文字を含むパスが往復で変化しました");
  }
  renderSnapshot({queue: queue.queue, state: state.state});
  elements.errorPanel.hidden = true;
  setConnection("ready", "SSE接続済み");
};

const showFailure = (error) => {
  elements.backendStatus.textContent = "接続失敗";
  elements.errorMessage.textContent = error.message;
  elements.errorPanel.hidden = false;
  setConnection("failed", "バックエンド停止");
};

const connectEvents = () => {
  const events = new EventSource("api/events");
  events.addEventListener("snapshot", (event) => {
    renderSnapshot(JSON.parse(event.data));
    elements.errorPanel.hidden = true;
    setConnection("ready", "SSE接続済み");
  });
  events.onerror = () => {
    showFailure(new Error("状態ストリームが切断されました"));
  };
};

elements.reload.addEventListener("click", () => loadSnapshot().catch(showFailure));

loadSnapshot()
  .then(connectEvents)
  .catch(showFailure);
