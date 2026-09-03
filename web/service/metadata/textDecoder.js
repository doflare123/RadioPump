// @ts-check

import { ByteView, ParseError } from "./binaryReader.js";

/** Убирает управляющие нули и пробелы из внешних текстовых тегов. */
export function cleanText(value) {
  return String(value ?? "").replaceAll("\0", "").trim();
}

/**
 * Декодирует текст ID3: Latin-1, UTF-16 с BOM, UTF-16BE или UTF-8.
 * Встроенный TextDecoder используется только с явно указанной кодировкой.
 */
export function decodeID3Text(bytes, encoding) {
  if (!bytes.length) return "";
  switch (encoding) {
    case 0: return cleanText(new TextDecoder("windows-1252").decode(bytes));
    case 1: {
      if (bytes.length < 2) return "";
      if (bytes[0] === 0xff && bytes[1] === 0xfe) return cleanText(new TextDecoder("utf-16le").decode(bytes.slice(2)));
      if (bytes[0] === 0xfe && bytes[1] === 0xff) return cleanText(decodeUTF16BE(bytes.slice(2)));
      return cleanText(new TextDecoder("utf-16le").decode(bytes));
    }
    case 2: return cleanText(decodeUTF16BE(bytes));
    case 3: return cleanText(new TextDecoder("utf-8").decode(bytes));
    default: throw new ParseError("Неизвестная кодировка ID3");
  }
}

/** Меняет порядок байтов UTF-16BE и декодирует через широко доступный UTF-16LE. */
function decodeUTF16BE(bytes) {
  const evenLength = bytes.length - (bytes.length % 2);
  const swapped = new Uint8Array(evenLength);
  for (let index = 0; index < evenLength; index += 2) {
    swapped[index] = bytes[index + 1];
    swapped[index + 1] = bytes[index];
  }
  return new TextDecoder("utf-16le").decode(swapped);
}

/** Возвращает позицию после строкового терминатора нужной ID3-кодировки. */
export function terminatedText(bytes, offset, encoding) {
  const wide = encoding === 1 || encoding === 2;
  const step = wide ? 2 : 1;
  for (let index = offset; index + step <= bytes.length; index += step) {
    if (bytes[index] === 0 && (!wide || bytes[index + 1] === 0)) {
      return { text: decodeID3Text(bytes.slice(offset, index), encoding), next: index + step };
    }
  }
  return { text: decodeID3Text(bytes.slice(offset), encoding), next: bytes.length };
}

/** Декодирует UTF-8 ключ/значение Vorbis Comment с безопасным fallback. */
export function decodeUTF8(bytes) {
  return cleanText(new TextDecoder("utf-8").decode(bytes));
}

/** Декодирует null-terminated UTF-16LE строку ASF ограниченного размера. */
export function decodeUTF16LE(bytes) {
  const length = bytes.length - (bytes.length % 2);
  return cleanText(new TextDecoder("utf-16le").decode(bytes.slice(0, length)));
}

/**
 * Разбирает vendor/comments Vorbis, FLAC, Opus и Speex в независимую map.
 * @param {Uint8Array} bytes @param {number} [offset]
 */
export function parseVorbisComments(bytes, offset = 0) {
  const view = new ByteView(bytes);
  if (offset + 8 > bytes.length) throw new ParseError("Обрезанный Vorbis Comment");
  const vendorLength = view.u32(offset, true);
  offset += 4;
  view.range(offset, vendorLength);
  offset += vendorLength;
  const count = view.u32(offset, true);
  offset += 4;
  if (count > 10000) throw new ParseError("Слишком много текстовых тегов");
  /** @type {Record<string, string[]>} */
  const tags = {};
  for (let index = 0; index < count; index += 1) {
    const length = view.u32(offset, true);
    offset += 4;
    const pair = decodeUTF8(view.slice(offset, length));
    offset += length;
    const separator = pair.indexOf("=");
    if (separator <= 0) continue;
    const key = normalizeKey(pair.slice(0, separator));
    const value = cleanText(pair.slice(separator + 1));
    if (value) (tags[key] ||= []).push(value);
  }
  return tags;
}

/** Приводит разные имена встроенных тегов к одному регистру без разделителей. */
export function normalizeKey(key) {
  return cleanText(key).toLowerCase().replace(/[_\- /]/g, "");
}

/**
 * Переносит общеизвестные имена тегов в единый результат, сохраняя первое значение.
 * @param {Record<string, string[]>} tags @param {import('./reader.js').TrackMetadata} metadata
 */
export function applyTags(tags, metadata) {
  const first = (...names) => {
    for (const name of names) {
      const value = tags[normalizeKey(name)]?.filter(Boolean).join("; ");
      if (value) return value;
    }
    return "";
  };
  metadata.title ||= first("title");
  metadata.artist ||= first("artist", "author", "performer");
  metadata.album ||= first("album");
  metadata.albumArtist ||= first("albumartist", "albumartists", "band");
  metadata.date ||= first("date", "year", "originaldate", "originalyear");
  metadata.genre ||= first("genre");
  metadata.trackNumber ||= first("tracknumber", "track");
  metadata.discNumber ||= first("discnumber", "disc", "disk");
  metadata.composer ||= first("composer");
  metadata.comment ||= first("comment", "description");
}

/** Проверяет magic bytes изображения и возвращает допустимый browser MIME. */
export function imageMIME(data) {
  if (data[0] === 0xff && data[1] === 0xd8 && data[2] === 0xff) return "image/jpeg";
  if (data[0] === 0x89 && data[1] === 0x50 && data[2] === 0x4e && data[3] === 0x47) return "image/png";
  const ascii = (start, end) => String.fromCharCode(...data.subarray(start, end));
  if (ascii(0, 3) === "GIF") return "image/gif";
  if (ascii(0, 4) === "RIFF" && ascii(8, 12) === "WEBP") return "image/webp";
  return "";
}

/** Сохраняет первую корректную обложку и отбрасывает изображения больше 10 MiB. */
export function applyCover(data, description, metadata) {
  if (metadata.cover || !data.length) return;
  const mimeType = imageMIME(data);
  metadata.hasCover = true;
  if (!mimeType || data.length > 10 * 1024 * 1024) {
    metadata.warnings.push("Обложка найдена, но слишком велика или имеет неподдерживаемый формат");
    return;
  }
  metadata.cover = { mimeType, data: data.slice(), description: cleanText(description) };
}
