// @ts-check
import { RadioPlayer } from "./radioPlayer.js";

const base = (window.RadioPumpAPIBase || "").replace(/\/$/, "");
const container = document.querySelector("[data-stations]");
const status = document.querySelector("[data-radio-status]");
const template = document.querySelector("#station-template");
const cards = new Map();
const audio = new Audio();
audio.volume = 0.8;
let snapshot = [];
let serverEpoch = Date.now();
let receivedAt = performance.now();
let lastSnapshot = 0;
let pollTimer;
let pollController;
let stopped = false;
const serverNow = () => serverEpoch + performance.now() - receivedAt;
const player = new RadioPlayer(audio, base, () => render(), () => performance.now());
player.serverNow = serverNow;
player.online = navigator.onLine;

// Текстовые поля никогда не интерпретируются как HTML из тегов файла/станции.
function node(tag, text, className = "") {
  const el = document.createElement(tag);
  el.textContent = text;
  el.className = className;
  return el;
}
function duration(seconds) {
  const value = Math.max(0, Math.floor(seconds || 0));
  return `${Math.floor(value / 60)}:${String(value % 60).padStart(2, "0")}`;
}

// Карточки создаются один раз: focus, раскрытая история и плеер переживают polling.
function cardFor(station) {
  if (cards.has(station.id)) return cards.get(station.id);
  const card = template.content.firstElementChild.cloneNode(true);
  card.querySelector("[data-name]").textContent = station.id;
  card.querySelector("[data-play]").addEventListener("click", () => player.select(station.id));
  const cover = card.querySelector("[data-cover]");
  cover.addEventListener("error", () => { cover.hidden = true; });
  container.append(card);
  cards.set(station.id, card);
  return card;
}

// Список содержит до пяти записей; подпись об отсутствии музыки не имитирует трек.
function trackList(list, tracks, empty) {
  const key = JSON.stringify(tracks);
  if (list.dataset.key === key) return;
  list.dataset.key = key;
  list.replaceChildren();
  if (!tracks.length) { list.append(node("li", empty, "muted")); return; }
  tracks.slice(0, 5).forEach((track, i) => {
    const li = node("li", "");
    const detail = node("div", "");
    detail.append(node("strong", track.title), node("p", [track.artist, track.album].filter(Boolean).join(" · ")));
    li.append(node("span", String(i + 1).padStart(2, "0")), detail);
    list.append(li);
  });
}

// У слушающей станции учитываем оценку буфера и историю переходов. Остальные
// карточки показывают серверный эфир; данные аудио здесь никогда не меняются.
function render() {
  for (const station of snapshot) {
    const card = cardFor(station);
    const selected = player.station === station.id;
    const playing = selected && player.phase === "playing" && !audio.paused;
    let track = station.current;
    let now = serverNow();
    if (selected && player.audibleOrigin !== null) {
      now = player.audibleOrigin + audio.currentTime * 1000;
      const timeline = [station.current, ...station.history].filter(Boolean);
      track = timeline.find(t => t.started_ms <= now && (!t.ended_ms || now < t.ended_ms)) || station.current;
    }
    card.classList.toggle("is-selected", selected);
    card.classList.toggle("is-playing", playing);
    const button = card.querySelector("[data-play]");
    button.textContent = selected ? "Остановить" : "Слушать";
    button.setAttribute("aria-label", `${selected ? "Остановить" : "Слушать"} ${station.id}`);
    button.setAttribute("aria-pressed", String(selected));
    const tags = card.querySelector("[data-tags]");
    const tagKey = JSON.stringify(station.tags);
    if (tags.dataset.key !== tagKey) {
      tags.dataset.key = tagKey;
      tags.replaceChildren(...(station.tags.length ? station.tags : ["Вся библиотека"]).map(t => node("span", t)));
    }
    card.querySelector("[data-live]").textContent = track ? (playing ? "ВЫ СЛУШАЕТЕ" : "ПРЯМОЙ ЭФИР") : "ОЖИДАЕМ МУЗЫКУ";
    card.querySelector("[data-title]").textContent = track?.title || "Станция готовится к эфиру";
    card.querySelector("[data-artist]").textContent = track?.artist || "";
    card.querySelector("[data-album]").textContent = track?.album || "";
    const elapsed = track ? Math.max(0, (now - track.started_ms) / 1000) : 0;
    card.querySelector("[data-elapsed]").textContent = duration(track?.duration ? Math.min(track.duration, elapsed) : elapsed);
    card.querySelector("[data-duration]").textContent = track?.duration ? duration(track.duration) : "—";
    card.querySelector("[data-progress]").value = track?.duration ? Math.min(1, elapsed / track.duration) : 0;
    const cover = card.querySelector("[data-cover]");
    const url = track?.cover_url ? `${base}${track.cover_url}` : "";
    if (cover.dataset.url !== url) {
      cover.dataset.url = url;
      cover.hidden = !url;
      if (url) cover.src = url; else cover.removeAttribute("src");
    }
    const labels = { connecting: "Подключаемся к эфиру…", buffering: "Буферизация…", reconnecting: "Восстанавливаем подключение…", offline: "Нет сети. Продолжим после подключения.", playing: "В прямом эфире" };
    card.querySelector("[data-connection]").textContent = selected ? labels[player.phase] || "" : "";
    // При задержке аудио текущий для слушателя трек ещё не должен быть в истории.
    const history = track ? station.history.filter(t => t.ended_ms <= track.started_ms) : station.history;
    const pending = track ? [...station.history].reverse().filter(t => t.started_ms > track.started_ms) : [];
    if (track && station.current && station.current.started_ms > track.started_ms) pending.push(station.current);
    trackList(card.querySelector("[data-history]"), history, "История появится после первого трека");
    trackList(card.querySelector("[data-queue]"), [...pending, ...station.queue].slice(0, 5), "Ожидаем подходящие треки");
  }
  if (player.phase === "blocked") status.textContent = "Браузер приостановил звук. Нажмите «Слушать».";
  else if (lastSnapshot && performance.now() - lastSnapshot > 10000) status.textContent = "Сведения об эфире устарели. Восстанавливаем связь…";
}

// Короткие запросы с таймаутом и единственным таймером. Сбой metadata не трогает audio.
async function poll() {
  if (stopped) return;
  pollController = new AbortController();
  const timeout = setTimeout(() => pollController.abort(), 8000);
  const started = performance.now();
  let delay = 2000;
  try {
    const response = await fetch(`${base}/api/radio`, { cache: "no-store", signal: pollController.signal });
    if (!response.ok) throw new Error("radio unavailable");
    const data = await response.json();
    snapshot = data.stations;
    receivedAt = performance.now();
    serverEpoch = Date.parse(data.server_time) + (receivedAt - started) / 2;
    lastSnapshot = receivedAt;
    for (const [id, card] of cards) {
      if (!snapshot.some(s => s.id === id)) {
        if (player.station === id) player.stop();
        card.remove(); cards.delete(id);
      }
    }
    status.textContent = snapshot.length ? "" : "Станции пока не настроены.";
    render();
  } catch {
    if (!stopped) status.textContent = "Не удалось обновить станции. Повторяем подключение…";
    delay = 5000;
  } finally {
    clearTimeout(timeout);
    if (!stopped) pollTimer = setTimeout(poll, delay);
  }
}

document.querySelector("[data-volume]").addEventListener("input", event => { audio.volume = Number(event.target.value); });
window.addEventListener("online", () => player.setOnline(true));
window.addEventListener("offline", () => player.setOnline(false));
const ticker = setInterval(render, 500);
window.addEventListener("pagehide", () => {
  stopped = true;
  clearTimeout(pollTimer); clearInterval(ticker);
  pollController?.abort(); player.destroy();
});
// После возврата из bfcache создаём чистые таймеры, без скрытых старых подписок.
window.addEventListener("pageshow", event => { if (event.persisted) location.reload(); });
poll();
