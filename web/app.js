const API_BASE = (window.RadioPumpAPIBase || "").replace(/\/$/, "");
const state = {
  tracks: [],
  tags: [],

};

document.addEventListener("DOMContentLoaded", () => {
  setupTheme();
  markActiveNav();


  const page = document.body.dataset.page;
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

  setupTagManagement();
  setupTrackEditor();
  await refreshTags();
  await setupUploadForm();
  await refreshAdminTracks();
}

// Загружает единый справочник и одновременно обновляет все tag selectors админки.
async function refreshTags() {
  const uploadSelected = selectedTagIDs(document.querySelector("[data-upload-tags]"));
  const editSelected = selectedTagIDs(document.querySelector("[data-edit-tags]"));
  const response = await authFetch("/api/admin/tags");
  const data = await readJSON(response);
  if (!response.ok) throw new Error(data.error || "Не удалось загрузить теги");
  state.tags = Array.isArray(data) ? data : [];
  renderTagOptions(document.querySelector("[data-upload-tags]"), uploadSelected);
  renderTagOptions(document.querySelector("[data-edit-tags]"), editSelected);
  renderTagManager();
}

// Рисует checkbox-список только из серверного справочника; имя не отправляется с треком.
function renderTagOptions(container, selectedIDs = []) {
  if (!container) return;
  const selected = new Set(selectedIDs.map(String));
  container.replaceChildren();
  if (state.tags.length === 0) {
    container.textContent = "Тегов пока нет";
    return;
  }
  for (const tag of state.tags) {
    const label = document.createElement("label");
    label.className = "tag-option";
    const input = document.createElement("input");
    input.type = "checkbox";
    input.name = "tag_ids";
    input.value = String(tag.ID);
    input.checked = selected.has(String(tag.ID));
    label.append(input, document.createTextNode(tag.Name));
    container.appendChild(label);
  }
}

// Возвращает выбранные ID из checkbox-контейнера; отсутствующий контейнер означает [].
function selectedTagIDs(container) {
  if (!container) return [];
  return [...container.querySelectorAll('input[name="tag_ids"]:checked')]
    .map((input) => Number(input.value))
    .filter((id) => Number.isSafeInteger(id) && id > 0);
}

// Подключает создание тега; rename/delete listeners создаются при каждом render списка.
function setupTagManagement() {
  const form = document.querySelector("[data-tag-form]");
  const status = document.querySelector("[data-tag-status]");
  if (!form || !status) return;
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    status.textContent = "Добавление…";
    const name = String(new FormData(form).get("name") || "");
    try {
      await mutateTag("/api/admin/tags", "POST", name);
      form.reset();
      status.textContent = "Тег добавлен";
      await refreshTags();
    } catch (error) {
      status.textContent = error.message;
    }
  });
}

// Строит редактируемые chips справочника безопасными DOM-операциями.
function renderTagManager() {
  const container = document.querySelector("[data-tag-manager]");
  const status = document.querySelector("[data-tag-status]");
  if (!container || !status) return;
  container.replaceChildren();
  if (state.tags.length === 0) {
    container.textContent = "Справочник пуст";
    return;
  }
  for (const tag of state.tags) {
    const editor = document.createElement("div");
    editor.className = "tag-editor";
    const input = document.createElement("input");
    input.value = tag.Name;
    input.maxLength = 64;
    input.setAttribute("aria-label", `Имя тега ${tag.Name}`);
    const save = document.createElement("button");
    save.className = "button";
    save.type = "button";
    save.textContent = "Сохранить";
    const remove = document.createElement("button");
    remove.className = "button button--danger";
    remove.type = "button";
    remove.textContent = "Удалить";
    // Rename оставляет ID прежним, поэтому назначенные трекам связи сохраняются.
    save.addEventListener("click", async () => {
      status.textContent = "Сохранение…";
      try {
        await mutateTag(`/api/admin/tags/${tag.ID}`, "PUT", input.value);
        status.textContent = "Тег переименован";
        await refreshTags();
        await refreshAdminTracks();
      } catch (error) { status.textContent = error.message; }
    });
    // Подтверждение сообщает о каскадном снятии тега со всех треков.
    remove.addEventListener("click", async () => {
      if (!confirm(`Удалить тег «${tag.Name}» со всех треков?`)) return;
      status.textContent = "Удаление…";
      try {
        const response = await authFetch(`/api/admin/tags/${tag.ID}`, { method: "DELETE" });
        const data = await readJSON(response);
        if (!response.ok) throw new Error(data.error || "Не удалось удалить тег");
        status.textContent = "Тег удалён";
        await refreshTags();
        await refreshAdminTracks();
      } catch (error) { status.textContent = error.message; }
    });
    editor.append(input, save, remove);
    container.appendChild(editor);
  }
}

// Отправляет одинаковый JSON-контракт для create и rename тега.
async function mutateTag(path, method, name) {
  const response = await authFetch(path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name }),
  });
  const data = await readJSON(response);
  if (!response.ok) throw new Error(data.error || "Не удалось изменить тег");
  return data;
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
      const cover = await preview.coverForUpload();
      if (cover) payload.append("cover", cover, "cover.jpg");
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
    tbody.innerHTML = `<tr><td colspan="5" class="muted">Треков пока нет</td></tr>`;
    return;
  }

  tracks.forEach((track) => {
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td>${escapeHTML(track.Title || "Без названия")}</td>
      <td>${escapeHTML(track.Artist || "—")}</td>
      <td>${escapeHTML(track.Album || "—")}</td>
      <td><div class="tag-list">${(track.Tags || []).map((tag) => `<span class="tag-chip">${escapeHTML(tag.Name)}</span>`).join("") || "—"}</div></td>
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
      editTrack(track);
    });
  });
}

// Открывает единый editor трека и отмечает его текущие связи с тегами.
function editTrack(track) {
  const dialog = document.querySelector("[data-track-dialog]");
  const form = document.querySelector("[data-edit-track-form]");
  if (!dialog || !form) return;
  form.elements.id.value = track.ID;
  form.elements.title.value = track.Title || "";
  form.elements.artist.value = track.Artist || "";
  form.elements.album.value = track.Album || "";
  form.elements.duration.value = track.Duration || 0;
  const status = document.querySelector("[data-edit-track-status]");
  if (status) status.textContent = "";
  renderTagOptions(document.querySelector("[data-edit-tags]"), (track.Tags || []).map((tag) => tag.ID));
  dialog.showModal();
}

// Подключает сохранение editor-а один раз, а содержимое формы меняется при открытии.
function setupTrackEditor() {
  const dialog = document.querySelector("[data-track-dialog]");
  const form = document.querySelector("[data-edit-track-form]");
  const status = document.querySelector("[data-edit-track-status]");
  if (!dialog || !form || !status) return;
  document.querySelector("[data-close-track-dialog]")?.addEventListener("click", () => dialog.close());
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    status.textContent = "Сохранение…";
    const payload = {
      title: form.elements.title.value,
      artist: form.elements.artist.value,
      album: form.elements.album.value,
      duration: Number(form.elements.duration.value) || 0,
      tag_ids: selectedTagIDs(form.querySelector("[data-edit-tags]")),
    };
    try {
      const response = await authFetch(`/api/admin/tracks/${form.elements.id.value}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });
      const data = await readJSON(response);
      if (!response.ok) throw new Error(data.error || "Не удалось обновить трек");
      dialog.close();
      status.textContent = "";
      await refreshAdminTracks();
    } catch (error) { status.textContent = error.message; }
  });
}

async function deleteTrack(id) {
  const response = await authFetch(`/api/admin/tracks/${id}`, { method: "DELETE" });
  if (response.status === 202) {
    alert("Трек убран из библиотеки. Сервер повторит удаление файла автоматически, когда файл станет доступен.");
  }
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
