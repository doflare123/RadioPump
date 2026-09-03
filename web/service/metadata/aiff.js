// @ts-check

import { BinaryFile, ByteView, ParseError } from "./binaryReader.js";
import { parseID3At } from "./id3.js";
import { cleanText } from "./textDecoder.js";

/** Разбирает FORM AIFF/AIFC, текстовые chunks, COMM и вложенный ID3. */
export async function parseAIFF(file, metadata) {
  const header = new ByteView(await file.read(0, 12));
  const kind = header.ascii(8, 4);
  if (header.ascii(0, 4) !== "FORM" || !["AIFF", "AIFC"].includes(kind)) throw new ParseError("Некорректный AIFF");
  metadata.container = kind;
  let frames = 0;
  let offset = 12;
  let chunks = 0;
  while (offset + 8 <= file.size && chunks++ < 10000) {
    const chunk = new ByteView(await file.read(offset, 8));
    const id = chunk.ascii(0, 4);
    const size = chunk.u32(4);
    if (size > file.size - offset - 8) throw new ParseError("AIFF chunk выходит за границу файла");
    const payload = offset + 8;
    if (id === "COMM") frames = await parseCommon(file, payload, size, kind, metadata);
    else if (id === "ID3 " && size >= 10) await parseID3At(file, payload, metadata);
    else if (["NAME", "AUTH", "ANNO", "(c) "].includes(id) && size <= 1024 * 1024) {
      const value = cleanText(new TextDecoder("windows-1252").decode(await file.read(payload, size)));
      if (id === "NAME") metadata.title ||= value;
      else if (id === "AUTH") metadata.artist ||= value;
      else metadata.comment ||= value;
    }
    offset = payload + size + (size & 1);
  }
  if (frames && metadata.sampleRate) metadata.duration ||= frames / metadata.sampleRate;
  if (metadata.sampleRate && metadata.channels && metadata.bitsPerSample) metadata.bitrate ||= metadata.sampleRate * metadata.channels * metadata.bitsPerSample;
  metadata.codec ||= kind === "AIFC" ? "AIFF-C audio" : "PCM";
  metadata.lossless = true;
}

/** Читает COMM и 80-битное extended число sample rate из формата IEEE 754 AIFF. */
async function parseCommon(file, offset, size, kind, metadata) {
  if (size < 18) throw new ParseError("Обрезанный COMM AIFF");
  const view = new ByteView(await file.read(offset, Math.min(size, 22)));
  metadata.channels = view.u16(0);
  const frames = view.u32(2);
  metadata.bitsPerSample = view.u16(6);
  metadata.sampleRate = extended80(view.slice(8, 10));
  if (kind === "AIFC" && size >= 22) {
    const code = view.ascii(18, 4);
    metadata.codec = code === "NONE" || code === "twos" || code === "sowt" ? "PCM" : code;
  }
  return frames;
}

/** Преобразует положительное 80-битное extended float, применяемое в COMM. */
function extended80(bytes) {
  const view = new ByteView(bytes);
  const exponent = view.u16(0) & 0x7fff;
  if (!exponent) return 0;
  const high = view.u32(2);
  const low = view.u32(6);
  return (high * 2 ** 32 + low) * 2 ** (exponent - 16383 - 63);
}
