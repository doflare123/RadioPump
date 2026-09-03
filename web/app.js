const API_BASE = (window.RadioPumpAPIBase || "").replace(/\/$/, "");
const WS_URL =
  window.RadioPumpWSUrl ||
  `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/ws`;

// HTTP: все запросы идут через API_BASE.
// Если сайт запущен отдельно на :5000, укажите в config.js:
// window.RadioPumpAPIBase = "http://localhost:8080";
// Если сайт отдается Go-сервером на :8080, API_BASE можно оставить пустым.
//
// WebSocket: переменная WS_URL уже готова для будущего endpoint /ws.
// Сейчас backend WebSocket еще не реализует.

const state = {
  tracks: [],
  currentTrack: null,
};

document.addEventListener("DOMContentLoaded", () => {
  setupTheme();
  markActiveNav();
  setupFooterPlayer();

  const page = document.body.dataset.page;
  if (page === "home") initHome();
  if (page === "library") initLibrary();
  if (page === "waves") initWaves();
  if (page === "player") initPlayerPage();
  if (page === "login") initLogin();
  if (page === "admin-tracks") initAdminTracks();
});

function setupTheme() {
  const stored = localStorage.getItem("radiopump-theme");
  document.documentElement.dataset.theme = stored || "light";

  document.querySelectorAll("[data-theme-toggle]").forEach((button) => {
    button.addEventListener("click", () => {
      const next = document.documentElement.dataset.theme === "dark" ? "light" : "dark";
      document.documentElement.dataset.theme = next;
      localStorage.setItem("radiopump-theme", next);
    });
  });
}

function markActiveNav() {
  const current = location.pathname.split("/").pop() || "index.html";
  document.querySelectorAll(".nav a").forEach((link) => {
    if (link.getAttribute("href") === current) {
      link.classList.add("is-active");
    }
  });
}

function setupFooterPlayer() {
  const stored = sessionStorage.getItem("radiopump-current-track");
  if (!stored) return;

  try {
    setCurrentTrack(JSON.parse(stored), false);
  } catch {
    sessionStorage.removeItem("radiopump-current-track");
  }
}

async function initHome() {
  const tracks = await loadTracks();
  renderTracks(document.querySelector("[data-latest-tracks]"), tracks.slice(0, 5));
  hydrateFirstPlayable(tracks);
}

async function initLibrary() {
  const tracks = await loadTracks();
  renderTracks(document.querySelector("[data-track-list]"), tracks);
}

async function initWaves() {
  const tracks = await loadTracks();
  renderTracks(document.querySelector("[data-wave-tracks]"), tracks.slice(0, 6));
  hydrateFirstPlayable(tracks);
}

async function initPlayerPage() {
  const tracks = await loadTracks();
  renderTracks(document.querySelector("[data-player-playlist]"), tracks);
  hydrateFirstPlayable(tracks);
}

function initLogin() {
  const form = document.querySelector("[data-login-form]");
  const status = document.querySelector("[data-login-status]");
  if (!form) return;

  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    status.textContent = "Вход...";

    const payload = Object.fromEntries(new FormData(form).entries());
    try {
      const response = await fetch(`${API_BASE}/api/auth/login`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });
      const data = await readJSON(response);
      if (!response.ok) throw new Error(data.error || "Не удалось войти");

      localStorage.setItem("radiopump-admin-token", data.token);
      status.textContent = "Готово";
      location.href = "admin-tracks.html";
    } catch (error) {
      status.textContent = error.message;
    }
  });
}

async function initAdminTracks() {
  if (!getToken()) {
    location.href = "admin-login.html";
    return;
  }

  try {
    const response = await authFetch("/api/auth/me");
    if (!response.ok) throw new Error("Сессия истекла");
  } catch {
    clearToken();
    location.href = "admin-login.html";
    return;
  }

  const logout = document.querySelector("[data-logout]");
  if (logout) {
    logout.addEventListener("click", () => {
      clearToken();
      location.href = "admin-login.html";
    });
  }

  await setupUploadForm();
  await refreshAdminTracks();
}

async function setupUploadForm() {
  const form = document.querySelector("[data-upload-form]");
  const status = document.querySelector("[data-upload-status]");
  if (!form) return;

  // Парсер нужен только в админке, поэтому публичные страницы его не загружают.
  const { setupMetadataPreview } = await import("./service/uploadMetadata.js");
  const preview = setupMetadataPreview(form);

  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    if (!preview.canUpload()) return;
    // FormData создаём до блокировки input: disabled-поля браузер в него не включает.
    const payload = new FormData(form);
    preview.setUploading(true);
    status.textContent = "Загрузка...";

    try {
      const response = await authFetch("/api/admin/tracks", {
        method: "POST",
        body: payload,
      });
      const data = await readJSON(response);
      if (!response.ok) throw new Error(data.error || "Не удалось загрузить трек");

      form.reset();
      status.textContent = "Трек загружен";
      await refreshAdminTracks();
    } catch (error) {
      status.textContent = error.message;
    } finally {
      preview.setUploading(false);
    }
  });
}

async function refreshAdminTracks() {
  const tracks = await loadTracks();
  const tbody = document.querySelector("[data-admin-tracks]");
  if (!tbody) return;

  tbody.innerHTML = "";
  if (tracks.length === 0) {
    tbody.innerHTML = `<tr><td colspan="4" class="muted">Треков пока нет</td></tr>`;
    return;
  }

  tracks.forEach((track) => {
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td>${escapeHTML(track.Title || "Без названия")}</td>
      <td>${escapeHTML(track.Artist || "—")}</td>
      <td>${escapeHTML(track.Album || "—")}</td>
      <td>
        <button class="button" type="button" data-edit="${track.ID}">Изменить</button>
        <button class="button button--danger" type="button" data-delete="${track.ID}">Удалить</button>
      </td>
    `;
    tbody.appendChild(tr);
  });

  tbody.querySelectorAll("[data-delete]").forEach((button) => {
    button.addEventListener("click", async () => {
      await deleteTrack(button.dataset.delete);
      await refreshAdminTracks();
    });
  });

  tbody.querySelectorAll("[data-edit]").forEach((button) => {
    button.addEventListener("click", async () => {
      const track = state.tracks.find((item) => String(item.ID) === button.dataset.edit);
      if (!track) return;
      await editTrack(track);
      await refreshAdminTracks();
    });
  });
}

async function editTrack(track) {
  const title = prompt("Название", track.Title || "");
  if (title === null) return;
  const artist = prompt("Исполнитель", track.Artist || "");
  if (artist === null) return;
  const album = prompt("Альбом", track.Album || "");
  if (album === null) return;

  const response = await authFetch(`/api/admin/tracks/${track.ID}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      title,
      artist,
      album,
      duration: track.Duration || 0,
    }),
  });
  const data = await readJSON(response);
  if (!response.ok) alert(data.error || "Не удалось обновить трек");
}

async function deleteTrack(id) {
  const response = await authFetch(`/api/admin/tracks/${id}`, { method: "DELETE" });
  if (!response.ok) {
    const data = await readJSON(response);
    alert(data.error || "Не удалось удалить трек");
  }
}

async function loadTracks() {
  try {
    const response = await authFetch("/api/tracks");
    const data = await readJSON(response);
    if (!response.ok) throw new Error(data.error || "Не удалось загрузить треки");
    state.tracks = Array.isArray(data) ? data : [];
    return state.tracks;
  } catch (error) {
    const target = document.querySelector("[data-error]");
    if (target) target.textContent = error.message;
    return [];
  }
}

function renderTracks(container, tracks) {
  if (!container) return;
  container.innerHTML = "";

  if (!tracks || tracks.length === 0) {
    container.innerHTML = `<div class="muted">Треков пока нет</div>`;
    return;
  }

  tracks.forEach((track) => {
    const row = document.createElement("div");
    row.className = "track-row";
    row.innerHTML = `
      <div>
        <div class="track-title">${escapeHTML(track.Title || "Без названия")}</div>
        <div class="track-meta">${escapeHTML(track.Artist || "Неизвестный исполнитель")}</div>
      </div>
      <button class="button button--primary" type="button">Слушать</button>
    `;
    row.querySelector("button").addEventListener("click", () => setCurrentTrack(track, true));
    container.appendChild(row);
  });
}

function hydrateFirstPlayable(tracks) {
  if (!state.currentTrack && tracks.length > 0) {
    setCurrentTrack(tracks[0], false);
  }
}

function setCurrentTrack(track, autoplay) {
  state.currentTrack = track;
  sessionStorage.setItem("radiopump-current-track", JSON.stringify(track));

  document.querySelectorAll("[data-current-title]").forEach((node) => {
    node.textContent = track.Title || "Без названия";
  });
  document.querySelectorAll("[data-current-artist]").forEach((node) => {
    node.textContent = track.Artist || "Неизвестный исполнитель";
  });
  document.querySelectorAll("audio[data-main-audio]").forEach((audio) => {
    const src = trackURL(track);
    if (audio.getAttribute("src") !== src) audio.setAttribute("src", src);
    if (autoplay) audio.play().catch(() => {});
  });
}

function trackURL(track) {
  const path = String(track.Path || "").replaceAll("\\", "/");
  if (path.startsWith("http://") || path.startsWith("https://") || path.startsWith("/")) {
    return path;
  }
  return `${API_BASE}/${path}`;
}

function getToken() {
  return localStorage.getItem("radiopump-admin-token") || "";
}

function clearToken() {
  localStorage.removeItem("radiopump-admin-token");
}

function authFetch(path, options = {}) {
  const headers = new Headers(options.headers || {});
  headers.set("Authorization", `Bearer ${getToken()}`);
  return fetch(`${API_BASE}${path}`, { ...options, headers });
}

async function readJSON(response) {
  const text = await response.text();
  if (!text) return {};
  try {
    return JSON.parse(text);
  } catch {
    return {};
  }
}

function escapeHTML(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}
