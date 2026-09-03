// @ts-check

import { BinaryFile, ByteView, MAX_BLOCK_BYTES, ParseError, ebmlVint } from "./binaryReader.js";
import { applyCover, applyTags, cleanText, normalizeKey } from "./textDecoder.js";

/** Master elements, внутрь которых безопасно заходить при поиске служебных данных. */
const MASTERS = new Set([0x1a45dfa3, 0x18538067, 0x1549a966, 0x1654ae6b, 0xae, 0xe1, 0x1254c367, 0x7373, 0x67c8, 0x1941a469, 0x61a7]);

/**
 * Разбирает служебную часть Matroska/WebM. Читается только ограниченный prefix:
 * обычные ffmpeg-файлы кладут Info/Tracks/Tags/Attachments до больших Clusters.
 */
export async function parseMatroska(file, metadata) {
  const length = Math.min(file.size, MAX_BLOCK_BYTES);
  const bytes = await file.read(0, length);
  const state = { scale: 1_000_000, durationTicks: 0, video: false, count: 0, tagName: "", fileName: "", mime: "" };
  /** @type {Record<string, string[]>} */
  const tags = {};
  parseElements(bytes, 0, bytes.length, 0, metadata, tags, state);
  if (state.video) throw new ParseError("Matroska/WebM содержит видеодорожку");
  if (state.durationTicks) metadata.duration = state.durationTicks * state.scale / 1_000_000_000;
  applyTags(tags, metadata);
  metadata.container = "Matroska/WebM";
  metadata.codec ||= "Matroska audio";
  metadata.lossless = /FLAC|ALAC|PCM/i.test(metadata.codec);
  if (file.size > length && !metadata.duration) metadata.warnings.push("Технические данные Matroska не найдены в начале файла");
}

/** Рекурсивно обходит известные master elements и обрабатывает только нужные leaf IDs. */
function parseElements(bytes, start, end, depth, metadata, tags, state) {
  if (depth > 12) throw new ParseError("Слишком глубокая структура EBML");
  let offset = start;
  while (offset < end) {
    if (++state.count > 20000) throw new ParseError("Слишком много EBML elements");
    let idInfo;
    let sizeInfo;
    try { idInfo = ebmlVint(bytes, offset, true); sizeInfo = ebmlVint(bytes, offset + idInfo.length); }
    catch { break; }
    const payload = offset + idInfo.length + sizeInfo.length;
    const declaredEnd = sizeInfo.unknown ? end : payload + sizeInfo.value;
    if (declaredEnd > end) break;
    const id = idInfo.value;
    if (MASTERS.has(id)) parseElements(bytes, payload, declaredEnd, depth + 1, metadata, tags, state);
    else processElement(id, bytes.slice(payload, declaredEnd), metadata, tags, state);
    offset = declaredEnd;
  }
}

/** Переносит технические EBML leaves, SimpleTag и attachment cover в состояние результата. */
function processElement(id, value, metadata, tags, state) {
  const view = new ByteView(value);
  if (id === 0x2ad7b1) state.scale = unsigned(value);
  else if (id === 0x4489) state.durationTicks = value.length === 4 ? view.f32(0) : value.length === 8 ? view.f64(0) : 0;
  else if (id === 0x83 && unsigned(value) === 1) state.video = true;
  else if (id === 0x86) metadata.codec = codecName(text(value));
  else if (id === 0xb5) metadata.sampleRate = value.length === 4 ? view.f32(0) : value.length === 8 ? view.f64(0) : 0;
  else if (id === 0x9f) metadata.channels = unsigned(value);
  else if (id === 0x6264) metadata.bitsPerSample = unsigned(value);
  else if (id === 0x7ba9) metadata.title ||= text(value);
  // SimpleTag leaves идут последовательно; временный ключ хранится только до TagString.
  else if (id === 0x45a3) state.tagName = text(value);
  else if (id === 0x4487 && state.tagName) { (tags[normalizeKey(state.tagName)] ||= []).push(text(value)); state.tagName = ""; }
  else if (id === 0x466e) state.fileName = text(value);
  else if (id === 0x4660) state.mime = text(value);
  else if (id === 0x465c && /^image\//i.test(state.mime || "")) applyCover(value, state.fileName || "", metadata);
}

/** Читает беззнаковое big-endian целое EBML длиной до 8 байт. */
function unsigned(bytes) {
  if (!bytes.length || bytes.length > 8) return 0;
  let value = 0;
  for (const byte of bytes) value = value * 256 + byte;
  return Number.isSafeInteger(value) ? value : 0;
}

/** Декодирует строковые EBML leaves как UTF-8. */
function text(bytes) { return cleanText(new TextDecoder("utf-8").decode(bytes)); }

/** Делает Matroska CodecID понятным пользователю без отдельной таблицы зависимостей. */
function codecName(id) {
  return ({ "A_OPUS": "Opus", "A_VORBIS": "Vorbis", "A_FLAC": "FLAC", "A_AAC": "AAC", "A_ALAC": "ALAC", "A_MPEG/L3": "MP3", "A_MPEG/L2": "MP2", "A_PCM/INT/LIT": "PCM", "A_PCM/INT/BIG": "PCM" })[id] || id;
}
