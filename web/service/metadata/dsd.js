// @ts-check

import { BinaryFile, ByteView, ParseError } from "./binaryReader.js";
import { parseID3At } from "./id3.js";

/** Разбирает DSF: fmt даёт точную длительность, а ID3 offset — встроенные теги. */
export async function parseDSF(file, metadata) {
  const header = new ByteView(await file.read(0, 28));
  if (header.ascii(0, 4) !== "DSD ") throw new ParseError("Некорректный DSF");
  const metadataOffset = header.u64(20, true);
  let offset = 28;
  let chunks = 0;
  while (offset + 12 <= file.size && chunks++ < 1000) {
    const chunk = new ByteView(await file.read(offset, 12));
    const id = chunk.ascii(0, 4);
    const size = chunk.u64(4, true);
    if (size < 12 || size > file.size - offset) throw new ParseError("DSF chunk выходит за границу файла");
    if (id === "fmt " && size >= 52) await parseDSFFormat(file, offset + 12, metadata);
    offset += size;
    if (metadataOffset && offset >= metadataOffset) break;
  }
  if (metadataOffset && metadataOffset + 10 <= file.size) await parseID3At(file, metadataOffset, metadata);
  metadata.container = "DSF";
  metadata.codec = "DSD";
  metadata.bitsPerSample ||= 1;
  metadata.lossless = true;
}

/** Читает channels, DSD sample rate, bits/sample и sample count из DSF fmt. */
async function parseDSFFormat(file, offset, metadata) {
  const view = new ByteView(await file.read(offset, 40));
  metadata.channels = view.u32(12, true);
  metadata.sampleRate = view.u32(16, true);
  metadata.bitsPerSample = view.u32(20, true);
  const samples = view.u64(24, true);
  if (metadata.sampleRate) metadata.duration = samples / metadata.sampleRate;
  metadata.bitrate = metadata.sampleRate * metadata.channels * metadata.bitsPerSample;
}

/** Разбирает DSDIFF/FRM8 chunks, PROP/SND properties и опциональный ID3 chunk. */
export async function parseDFF(file, metadata) {
  const header = new ByteView(await file.read(0, 16));
  if (header.ascii(0, 4) !== "FRM8" || header.ascii(12, 4) !== "DSD ") throw new ParseError("Некорректный DSDIFF");
  let audioBytes = 0;
  let offset = 16;
  let chunks = 0;
  while (offset + 12 <= file.size && chunks++ < 10000) {
    const chunk = new ByteView(await file.read(offset, 12));
    const id = chunk.ascii(0, 4);
    const size = chunk.u64(4);
    if (size > file.size - offset - 12) throw new ParseError("DSDIFF chunk выходит за границу файла");
    const payload = offset + 12;
    if (id === "PROP" && size <= 1024 * 1024) await parseDFFProperties(file, payload, size, metadata);
    else if (id === "DSD ") audioBytes = size;
    else if (id === "ID3 " && size >= 10) await parseID3At(file, payload, metadata);
    offset = payload + size + (size & 1);
  }
  if (audioBytes && metadata.sampleRate && metadata.channels) metadata.duration = audioBytes * 8 / (metadata.sampleRate * metadata.channels);
  metadata.container = "DSDIFF";
  metadata.codec = "DSD";
  metadata.bitsPerSample = 1;
  metadata.bitrate = metadata.sampleRate && metadata.channels ? metadata.sampleRate * metadata.channels : 0;
  metadata.lossless = true;
}

/** Обходит вложенные PROP/SND свойства FS, CHNL и CMPR. */
async function parseDFFProperties(file, offset, size, metadata) {
  if (size < 4) return;
  const bytes = await file.read(offset, size);
  const view = new ByteView(bytes);
  if (view.ascii(0, 4) !== "SND ") return;
  let cursor = 4;
  while (cursor + 12 <= bytes.length) {
    const id = view.ascii(cursor, 4);
    const length = view.u64(cursor + 4);
    cursor += 12;
    if (length > bytes.length - cursor) throw new ParseError("Обрезанный DSDIFF property");
    if (id === "FS  " && length >= 4) metadata.sampleRate = view.u32(cursor);
    else if (id === "CHNL" && length >= 2) metadata.channels = view.u16(cursor);
    cursor += length + (length & 1);
  }
}
