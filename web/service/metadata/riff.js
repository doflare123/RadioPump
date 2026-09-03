// @ts-check

import { BinaryFile, ByteView, ParseError } from "./binaryReader.js";
import { parseID3At } from "./id3.js";
import { applyTags, cleanText } from "./textDecoder.js";

/**
 * Разбирает RIFF/RF64 WAVE по заголовкам chunk, не читая большой блок PCM/audio data.
 * @param {BinaryFile} file @param {import('./reader.js').TrackMetadata} metadata
 */
export async function parseRIFF(file, metadata) {
  const header = new ByteView(await file.read(0, 12));
  if (!(["RIFF", "RF64"].includes(header.ascii(0, 4))) || header.ascii(8, 4) !== "WAVE") throw new ParseError("Некорректный WAVE");
  metadata.container = header.ascii(0, 4) === "RF64" ? "RF64/WAVE" : "RIFF/WAVE";
  /** @type {Record<string, string[]>} */
  const tags = {};
  let dataBytes = 0;
  let offset = 12;
  let chunks = 0;
  while (offset + 8 <= file.size && chunks++ < 10000) {
    const chunk = new ByteView(await file.read(offset, 8));
    const id = chunk.ascii(0, 4);
    let size = chunk.u32(4, true);
    if (size === 0xffffffff) size = Math.max(0, file.size - offset - 8);
    if (size > file.size - offset - 8) throw new ParseError("WAVE chunk выходит за границу файла");
    const payload = offset + 8;
    if (id === "fmt ") await parseFormat(file, payload, size, metadata);
    else if (id === "data") dataBytes = size;
    else if (id === "LIST") await parseInfo(file, payload, size, tags);
    else if (/^id3 /i.test(id) && size >= 10) await parseID3At(file, payload, metadata);
    offset = payload + size + (size & 1);
  }
  applyTags(tags, metadata);
  if (dataBytes && metadata.sampleRate && metadata.channels && metadata.bitsPerSample) {
    metadata.duration ||= dataBytes / (metadata.sampleRate * metadata.channels * metadata.bitsPerSample / 8);
    metadata.bitrate ||= metadata.sampleRate * metadata.channels * metadata.bitsPerSample;
  }
  metadata.codec ||= "WAVE audio";
  metadata.lossless = true;
}

/** Извлекает WAVEFORMATEX и различает основные PCM/float/compressed codec IDs. */
async function parseFormat(file, offset, size, metadata) {
  if (size < 16) throw new ParseError("Обрезанный fmt WAVE");
  const view = new ByteView(await file.read(offset, Math.min(size, 40)));
  const format = view.u16(0, true);
  metadata.channels = view.u16(2, true);
  metadata.sampleRate = view.u32(4, true);
  metadata.bitrate = view.u32(8, true) * 8;
  metadata.bitsPerSample = view.u16(14, true);
  const names = { 1: "PCM", 3: "IEEE Float", 6: "A-law", 7: "mu-law", 0x55: "MP3", 0xfffe: "WAVE Extensible" };
  metadata.codec = names[format] || `WAVE format 0x${format.toString(16)}`;
}

/** Разбирает LIST/INFO как набор четырёхсимвольных текстовых subchunks. */
async function parseInfo(file, offset, size, tags) {
  if (size < 4 || size > 16 * 1024 * 1024) return;
  const bytes = await file.read(offset, size);
  const view = new ByteView(bytes);
  if (view.ascii(0, 4) !== "INFO") return;
  const mapping = { INAM: "title", IART: "artist", IPRD: "album", ICRD: "date", IGNR: "genre", ITRK: "tracknumber", ICMT: "comment" };
  let cursor = 4;
  while (cursor + 8 <= bytes.length) {
    const id = view.ascii(cursor, 4);
    const length = view.u32(cursor + 4, true);
    cursor += 8;
    if (length > bytes.length - cursor) throw new ParseError("Обрезанный LIST/INFO");
    const name = mapping[id];
    const value = cleanText(new TextDecoder("windows-1252").decode(view.slice(cursor, length)));
    if (name && value) (tags[name] ||= []).push(value);
    cursor += length + (length & 1);
  }
}
