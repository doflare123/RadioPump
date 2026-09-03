// @ts-check

import { BinaryFile, ByteView, MAX_BLOCK_BYTES, ParseError, synchsafe32 } from "./binaryReader.js";
import { applyCover, applyTags, cleanText, decodeID3Text, normalizeKey, terminatedText } from "./textDecoder.js";

/** Соответствие текстовых ID3 frames единому набору полей RadioPump. */
const TEXT_FRAMES = {
  TIT2: "title", TT2: "title", TPE1: "artist", TP1: "artist",
  TALB: "album", TAL: "album", TPE2: "albumartist", TP2: "albumartist",
  TCON: "genre", TCO: "genre", TDRC: "date", TYER: "date", TYE: "date",
  TRCK: "tracknumber", TRK: "tracknumber", TPOS: "discnumber", TPA: "discnumber",
  TCOM: "composer", TCM: "composer",
};

/**
 * Разбирает MP3: ID3v2 в начале, параметры первого MPEG frame и ID3v1 в конце.
 * Длительность вычисляется по среднему bitrate, если Xing/VBR-заголовок недоступен.
 * @param {BinaryFile} file @param {import('./reader.js').TrackMetadata} metadata
 */
export async function parseMP3(file, metadata) {
  const prefix = await file.read(0, Math.min(file.size, 10));
  let audioOffset = 0;
  if (prefix.length >= 10 && ascii(prefix, 0, 3) === "ID3") {
    audioOffset = await parseID3At(file, 0, metadata);
  }
  await parseMPEGHeader(file, audioOffset, metadata);
  await parseID3v1(file, metadata);
  metadata.container = "MPEG";
}

/**
 * Разбирает raw AAC/ADTS: параметры берутся из первого frame, длительность —
 * из подсчёта размеров последовательных frames. ID3 в начале применяется как тег.
 */
export async function parseADTS(file, metadata) {
  let offset = 0;
  const prefix = await file.read(0, Math.min(file.size, 10));
  if (prefix.length >= 10 && ascii(prefix, 0, 3) === "ID3") offset = await parseID3At(file, 0, metadata);
  let frames = 0;
  let samples = 0;
  let cursor = offset;
  while (cursor + 7 <= file.size) {
    const header = new ByteView(await file.read(cursor, 7));
    if (header.u8(0) !== 0xff || (header.u8(1) & 0xf6) !== 0xf0) {
      if (!frames && cursor - offset < 4096) { cursor += 1; continue; }
      break;
    }
    const rateIndex = (header.u8(2) >>> 2) & 15;
    const rates = [96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350];
    const frameLength = ((header.u8(3) & 3) << 11) | (header.u8(4) << 3) | (header.u8(5) >>> 5);
    if (!rates[rateIndex] || frameLength < 7 || frameLength > file.size - cursor) throw new ParseError("Некорректный ADTS frame");
    metadata.sampleRate ||= rates[rateIndex];
    metadata.channels ||= ((header.u8(2) & 1) << 2) | (header.u8(3) >>> 6);
    samples += 1024 * ((header.u8(6) & 3) + 1);
    frames += 1;
    cursor += frameLength;
  }
  if (!frames || !metadata.sampleRate) throw new ParseError("AAC/ADTS frame отсутствует");
  metadata.duration = samples / metadata.sampleRate;
  metadata.bitrate = Math.round((cursor - offset) * 8 / metadata.duration);
  metadata.container = "ADTS";
  metadata.codec = "AAC";
}

/**
 * Разбирает самостоятельный блок ID3v2 по абсолютному offset и возвращает конец блока.
 * Используется также внутри WAV, AIFF, DSF и DFF.
 */
export async function parseID3At(file, offset, metadata) {
  const header = await file.read(offset, 10);
  if (ascii(header, 0, 3) !== "ID3") throw new ParseError("Сигнатура ID3 отсутствует");
  const major = header[3];
  if (major < 2 || major > 4) throw new ParseError(`Версия ID3v2.${major} не поддерживается`);
  const flags = header[5];
  const size = synchsafe32(header, 6);
  if (size > MAX_BLOCK_BYTES) throw new ParseError("ID3-тег слишком большой");
  let body = await file.read(offset + 10, size);
  if (flags & 0x80) body = removeUnsynchronization(body);
  let cursor = extendedHeaderLength(body, major, flags);
  /** @type {Record<string, string[]>} */
  const tags = {};
  while (cursor < body.length) {
    const frame = readFrameHeader(body, cursor, major);
    if (!frame || frame.size === 0) break;
    cursor += frame.headerSize;
    if (frame.size > body.length - cursor) throw new ParseError("ID3 frame выходит за границу тега");
    let data = body.slice(cursor, cursor + frame.size);
    cursor += frame.size;
    if (major === 4 && frame.unsynchronized) data = removeUnsynchronization(data);
    if (frame.compressed || frame.encrypted) continue;
    parseID3Frame(frame.id, data, tags, metadata);
  }
  applyTags(tags, metadata);
  return offset + 10 + size + (major === 4 && (flags & 0x10) ? 10 : 0);
}

/** Пропускает extended header ID3v2.3/v2.4 после проверки его размера. */
function extendedHeaderLength(body, major, flags) {
  if (!(flags & 0x40) || major === 2) return 0;
  if (body.length < 4) throw new ParseError("Обрезанный extended header ID3");
  const view = new ByteView(body);
  const declared = major === 4 ? synchsafe32(body, 0) : view.u32(0);
  const total = major === 3 ? declared + 4 : declared;
  if (total < 4 || total > body.length) throw new ParseError("Некорректный extended header ID3");
  return total;
}

/** Читает заголовки ID3v2.2 (6 байт) и ID3v2.3/2.4 (10 байт). */
function readFrameHeader(body, offset, major) {
  const headerSize = major === 2 ? 6 : 10;
  if (offset + headerSize > body.length) return null;
  const idLength = major === 2 ? 3 : 4;
  const id = ascii(body, offset, idLength);
  if (/^\x00+$/.test(id)) return null;
  if (!/^[A-Z0-9]{3,4}$/.test(id)) throw new ParseError("Некорректный идентификатор ID3 frame");
  const view = new ByteView(body);
  const size = major === 2 ? view.u24(offset + 3) : major === 4 ? synchsafe32(body, offset + 4) : view.u32(offset + 4);
  const formatFlags = major === 2 ? 0 : body[offset + 9];
  return {
    id, size, headerSize,
    compressed: major === 3 ? Boolean(formatFlags & 0x80) : Boolean(formatFlags & 0x08),
    encrypted: major === 3 ? Boolean(formatFlags & 0x40) : Boolean(formatFlags & 0x04),
    unsynchronized: major === 4 && Boolean(formatFlags & 0x02),
  };
}

/** Направляет известный ID3 frame в текстовые теги, комментарий или обложку. */
function parseID3Frame(id, data, tags, metadata) {
  if (TEXT_FRAMES[id] && data.length) {
    // Ноль разделяет несколько значений; обычный slash остаётся частью названия вроде AC/DC.
    const values = decodeID3Text(data.slice(1), data[0]).split("\0").map(cleanText).filter(Boolean);
    if (values.length) tags[normalizeKey(TEXT_FRAMES[id])] = values;
    return;
  }
  if ((id === "COMM" || id === "COM") && data.length >= 4) {
    const encoding = data[0];
    const description = terminatedText(data, 4, encoding);
    const value = decodeID3Text(data.slice(description.next), encoding);
    if (value) (tags.comment ||= []).push(value);
    return;
  }
  if (id === "APIC") parseAPIC(data, metadata);
  if (id === "PIC") parsePIC(data, metadata);
}

/** Извлекает ID3v2.3/v2.4 APIC после MIME, picture type и описания. */
function parseAPIC(data, metadata) {
  if (data.length < 5) return;
  const encoding = data[0];
  let cursor = 1;
  while (cursor < data.length && data[cursor] !== 0) cursor += 1;
  if (cursor >= data.length - 2) return;
  cursor += 2; // null MIME + picture type
  const description = terminatedText(data, cursor, encoding);
  applyCover(data.slice(description.next), description.text, metadata);
}

/** Извлекает компактный ID3v2.2 PIC с трёхбуквенным форматом изображения. */
function parsePIC(data, metadata) {
  if (data.length < 6) return;
  const encoding = data[0];
  const description = terminatedText(data, 5, encoding);
  applyCover(data.slice(description.next), description.text, metadata);
}

/** Удаляет вставленный 00 после FF согласно механизму unsynchronization ID3. */
function removeUnsynchronization(data) {
  const result = [];
  for (let index = 0; index < data.length; index += 1) {
    result.push(data[index]);
    if (data[index] === 0xff && data[index + 1] === 0x00) index += 1;
  }
  return Uint8Array.from(result);
}

/** Читает ID3v1 только как fallback, не перезаписывая более новые ID3v2 поля. */
async function parseID3v1(file, metadata) {
  if (file.size < 128) return;
  const bytes = await file.read(file.size - 128, 128);
  if (ascii(bytes, 0, 3) !== "TAG") return;
  const decode = (offset, length) => cleanText(new TextDecoder("windows-1252").decode(bytes.slice(offset, offset + length)));
  metadata.title ||= decode(3, 30);
  metadata.artist ||= decode(33, 30);
  metadata.album ||= decode(63, 30);
  metadata.date ||= decode(93, 4);
  if (!metadata.trackNumber && bytes[125] === 0 && bytes[126] !== 0) metadata.trackNumber = String(bytes[126]);
}

/** Ищет первый валидный MPEG frame и вычисляет технические поля и приблизительную длительность. */
async function parseMPEGHeader(file, start, metadata) {
  const length = Math.min(file.size - start, 1024 * 1024);
  if (length < 4) throw new ParseError("MPEG audio frame отсутствует");
  const bytes = await file.read(start, length);
  for (let index = 0; index + 4 <= bytes.length; index += 1) {
    const value = (bytes[index] * 0x1000000) + (bytes[index + 1] << 16) + (bytes[index + 2] << 8) + bytes[index + 3];
    if ((((value >>> 0) & 0xffe00000) >>> 0) !== 0xffe00000) continue;
    const versionBits = (value >>> 19) & 3;
    const layerBits = (value >>> 17) & 3;
    const bitrateIndex = (value >>> 12) & 15;
    const rateIndex = (value >>> 10) & 3;
    if (versionBits === 1 || layerBits === 0 || bitrateIndex === 0 || bitrateIndex === 15 || rateIndex === 3) continue;
    const version = versionBits === 3 ? 1 : versionBits === 2 ? 2 : 2.5;
    const layer = 4 - layerBits;
    const bitrate = mpegBitrate(version, layer, bitrateIndex);
    const baseRate = [44100, 48000, 32000][rateIndex];
    const sampleRate = version === 1 ? baseRate : version === 2 ? baseRate / 2 : baseRate / 4;
    metadata.codec = `MPEG ${version} Layer ${layer}`;
    metadata.bitrate = bitrate * 1000;
    metadata.sampleRate = sampleRate;
    metadata.channels = ((value >>> 6) & 3) === 3 ? 1 : 2;
    metadata.duration ||= Math.max(0, (file.size - start) * 8 / metadata.bitrate);
    return;
  }
  throw new ParseError("MPEG audio frame отсутствует");
}

/** Возвращает bitrate MPEG Layer I/II/III из стандартизованной таблицы индексов. */
function mpegBitrate(version, layer, index) {
  const v1 = {
    1: [0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448],
    2: [0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384],
    3: [0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320],
  };
  const v2 = {
    1: [0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256],
    2: [0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160],
    3: [0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160],
  };
  return (version === 1 ? v1 : v2)[layer][index];
}

/** Преобразует небольшой ASCII-диапазон без TextDecoder и скрытых кодировок. */
function ascii(bytes, offset, length) {
  return String.fromCharCode(...bytes.slice(offset, offset + length));
}
