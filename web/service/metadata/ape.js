// @ts-check

import { BinaryFile, ByteView, MAX_BLOCK_BYTES, ParseError } from "./binaryReader.js";
import { applyCover, applyTags, cleanText, normalizeKey } from "./textDecoder.js";

/** Разбирает Monkey's Audio и его APEv2 footer. */
export async function parseAPE(file, metadata) {
  metadata.container = "APE";
  metadata.codec = "Monkey's Audio";
  metadata.lossless = true;
  await parseAPEv2(file, metadata);
}

/** Разбирает WavPack header и общие APEv2-теги в конце файла. */
export async function parseWavPack(file, metadata) {
  const view = new ByteView(await file.read(0, 32));
  if (view.ascii(0, 4) !== "wvpk") throw new ParseError("Некорректный WavPack");
  const samples = view.u32(12, true);
  const flags = view.u32(24, true);
  const rates = [6000, 8000, 9600, 11025, 12000, 16000, 22050, 24000, 32000, 44100, 48000, 64000, 88200, 96000, 192000, 0];
  metadata.sampleRate = rates[(flags >>> 23) & 15];
  metadata.channels = flags & 4 ? 1 : 2;
  metadata.bitsPerSample = [8, 16, 24, 32][flags & 3];
  if (samples !== 0xffffffff && metadata.sampleRate) metadata.duration = samples / metadata.sampleRate;
  metadata.container = "WavPack";
  metadata.codec = "WavPack";
  metadata.lossless = true;
  await parseAPEv2(file, metadata);
}

/** Разбирает Musepack signature и APEv2; технические SV8 packets остаются необязательными. */
export async function parseMusepack(file, metadata) {
  metadata.container = "Musepack";
  metadata.codec = "Musepack";
  await parseAPEv2(file, metadata);
}

/** Ищет footer перед концом файла или перед ID3v1 и ограниченно читает items APEv2. */
export async function parseAPEv2(file, metadata) {
  if (file.size < 32) return;
  let footerOffset = file.size - 32;
  let footer = await file.read(footerOffset, 32);
  if (new ByteView(footer).ascii(0, 8) !== "APETAGEX" && file.size >= 160) {
    footerOffset = file.size - 160;
    footer = await file.read(footerOffset, 32);
  }
  const footerView = new ByteView(footer);
  if (footerView.ascii(0, 8) !== "APETAGEX") return;
  const tagSize = footerView.u32(12, true);
  const count = footerView.u32(16, true);
  if (tagSize < 32 || tagSize > MAX_BLOCK_BYTES || tagSize > footerOffset + 32 || count > 10000) throw new ParseError("Некорректный APEv2 footer");
  const start = footerOffset + 32 - tagSize;
  const bytes = await file.read(start, tagSize - 32);
  const view = new ByteView(bytes);
  /** @type {Record<string, string[]>} */
  const tags = {};
  let cursor = view.ascii(0, Math.min(8, view.length)) === "APETAGEX" ? 32 : 0;
  for (let index = 0; index < count && cursor < bytes.length; index += 1) {
    if (cursor + 8 > bytes.length) throw new ParseError("Обрезанный APEv2 item");
    const valueSize = view.u32(cursor, true);
    const flags = view.u32(cursor + 4, true);
    cursor += 8;
    let keyEnd = cursor;
    while (keyEnd < bytes.length && bytes[keyEnd] !== 0) keyEnd += 1;
    if (keyEnd === bytes.length || valueSize > bytes.length - keyEnd - 1) throw new ParseError("APEv2 item выходит за границу");
    const key = cleanText(new TextDecoder("utf-8").decode(view.slice(cursor, keyEnd - cursor)));
    cursor = keyEnd + 1;
    const value = view.slice(cursor, valueSize); cursor += valueSize;
    const type = (flags >>> 1) & 3;
    if (type === 1 && /^cover art/i.test(key)) {
      const separator = value.indexOf(0);
      if (separator >= 0) applyCover(value.slice(separator + 1), key, metadata);
    } else if (type === 0) {
      const values = new TextDecoder("utf-8").decode(value).split("\0").map(cleanText).filter(Boolean);
      if (values.length) tags[normalizeKey(key)] = values;
    }
  }
  applyTags(tags, metadata);
}
