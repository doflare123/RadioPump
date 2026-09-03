// @ts-check

import { BinaryFile, ByteView, MAX_BLOCK_BYTES, ParseError } from "./binaryReader.js";
import { applyCover, applyTags, cleanText } from "./textDecoder.js";

/** Ограничения обхода атомов не дают повреждённому MP4 создать бесконечную рекурсию. */
const MAX_DEPTH = 12;
const MAX_ATOMS = 10000;

/**
 * Разбирает ISO BMFF/M4A: длительность из mvhd, аудиоформат из stsd и теги ilst.
 * Большие media-data атомы только пропускаются, поэтому звук целиком в память не читается.
 * @param {BinaryFile} file @param {import('./reader.js').TrackMetadata} metadata
 */
export async function parseMP4(file, metadata) {
  const state = { count: 0, video: false };
  await walkAtoms(file, 0, file.size, 0, metadata, state);
  if (state.video) throw new ParseError("MP4 содержит видеодорожку и не является музыкальным файлом");
  metadata.container = "MP4/M4A";
  metadata.codec ||= "AAC/MP4 audio";
  metadata.lossless = /alac/i.test(metadata.codec);
}

/** Обходит только известные контейнерные атомы и проверяет каждый размер относительно родителя. */
async function walkAtoms(file, start, end, depth, metadata, state) {
  if (depth > MAX_DEPTH) throw new ParseError("Слишком глубокая структура MP4");
  let offset = start;
  while (offset + 8 <= end) {
    if (++state.count > MAX_ATOMS) throw new ParseError("Слишком много атомов MP4");
    const header = await file.read(offset, Math.min(16, end - offset));
    const view = new ByteView(header);
    let size = view.u32(0);
    const type = view.ascii(4, 4);
    let headerSize = 8;
    if (size === 1) { if (header.length < 16) throw new ParseError("Обрезанный 64-битный атом MP4"); size = view.u64(8); headerSize = 16; }
    if (size === 0) size = end - offset;
    if (size < headerSize || size > end - offset) throw new ParseError("Атом MP4 выходит за границу контейнера");
    const payload = offset + headerSize;
    const payloadSize = size - headerSize;
    if (type === "mvhd") await parseMovieHeader(file, payload, payloadSize, metadata);
    if (type === "hdlr") await parseHandler(file, payload, payloadSize, state);
    if (type === "stsd") await parseSampleDescription(file, payload, payloadSize, metadata);
    if (type === "ilst") await parseItemList(file, payload, payloadSize, metadata);
    const containers = new Set(["moov", "trak", "mdia", "minf", "stbl", "udta"]);
    if (containers.has(type)) await walkAtoms(file, payload, payload + payloadSize, depth + 1, metadata, state);
    if (type === "meta" && payloadSize >= 4) await walkAtoms(file, payload + 4, payload + payloadSize, depth + 1, metadata, state);
    offset += size;
  }
}

/** Читает timescale/duration из двух версий movie header. */
async function parseMovieHeader(file, offset, length, metadata) {
  const bytes = await file.read(offset, Math.min(length, 32));
  const view = new ByteView(bytes);
  const version = view.u8(0);
  const timescaleOffset = version === 1 ? 20 : 12;
  const durationOffset = version === 1 ? 24 : 16;
  const needed = version === 1 ? 32 : 20;
  if (bytes.length < needed) throw new ParseError("Обрезанный mvhd MP4");
  const timescale = view.u32(timescaleOffset);
  const duration = version === 1 ? view.u64(durationOffset) : view.u32(durationOffset);
  if (timescale) metadata.duration ||= duration / timescale;
}

/** Помечает video handler, чтобы не принять обычный видеоролик за музыкальный MP4. */
async function parseHandler(file, offset, length, state) {
  if (length < 12) return;
  const bytes = await file.read(offset, 12);
  if (new ByteView(bytes).ascii(8, 4) === "vide") state.video = true;
}

/** Извлекает fourcc, каналы, разрядность и sample rate первой audio sample entry. */
async function parseSampleDescription(file, offset, length, metadata) {
  if (length < 24) return;
  const bytes = await file.read(offset, Math.min(length, 48));
  const view = new ByteView(bytes);
  if (!view.u32(4)) return;
  const entrySize = view.u32(8);
  if (entrySize < 28 || entrySize > length - 8) return;
  const codec = view.ascii(12, 4).trim();
  metadata.codec = codec === "mp4a" ? "AAC" : codec === "alac" ? "ALAC" : codec;
  if (bytes.length >= 36) {
    metadata.channels ||= view.u16(32);
    metadata.bitsPerSample ||= view.u16(34);
  }
  if (bytes.length >= 44) metadata.sampleRate ||= view.u32(40) / 65536;
}

/** Читает небольшие ilst items и переносит Apple-ключи в единый набор тегов. */
async function parseItemList(file, start, length, metadata) {
  const end = start + length;
  /** @type {Record<string, string[]>} */
  const tags = {};
  let offset = start;
  while (offset + 8 <= end) {
    const itemHeader = new ByteView(await file.read(offset, 8));
    const itemSize = itemHeader.u32(0);
    const key = itemHeader.ascii(4, 4);
    if (itemSize < 16 || itemSize > end - offset) break;
    const dataHeader = new ByteView(await file.read(offset + 8, 16));
    const dataSize = dataHeader.u32(0);
    if (dataHeader.ascii(4, 4) !== "data" || dataSize < 16 || dataSize > itemSize - 8) { offset += itemSize; continue; }
    const valueLength = dataSize - 16;
    if (valueLength > MAX_BLOCK_BYTES) throw new ParseError("Тег MP4 слишком большой");
    const value = await file.read(offset + 24, valueLength);
    applyMP4Item(key, dataHeader.u32(8), value, tags, metadata);
    offset += itemSize;
  }
  applyTags(tags, metadata);
}

/** Декодирует текстовые, числовые и cover-art значения Apple ilst. */
function applyMP4Item(key, type, value, tags, metadata) {
  const mapping = { "\xa9nam": "title", "\xa9ART": "artist", "aART": "albumartist", "\xa9alb": "album", "\xa9day": "date", "\xa9gen": "genre", "\xa9wrt": "composer", "\xa9cmt": "comment" };
  if (key === "covr") { applyCover(value, "", metadata); return; }
  if ((key === "trkn" || key === "disk") && value.length >= 4) {
    const number = new ByteView(value).u16(2);
    if (number) (tags[key === "trkn" ? "tracknumber" : "discnumber"] ||= []).push(String(number));
    return;
  }
  const name = mapping[key];
  if (!name) return;
  const text = cleanText(new TextDecoder(type === 2 ? "utf-16be" : "utf-8").decode(value));
  if (text) (tags[name] ||= []).push(text);
}
