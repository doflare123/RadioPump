// @ts-check

import { BinaryFile, ByteView, ParseError } from "./binaryReader.js";
import { applyCover, applyTags, decodeUTF8, parseVorbisComments } from "./textDecoder.js";

/**
 * Последовательно читает FLAC metadata blocks до первого audio frame.
 * STREAMINFO даёт точную длительность, VORBIS_COMMENT — теги, PICTURE — обложку.
 * @param {BinaryFile} file @param {import('./reader.js').TrackMetadata} metadata
 */
export async function parseFLAC(file, metadata) {
  const magic = await file.read(0, 4);
  if (String.fromCharCode(...magic) !== "fLaC") throw new ParseError("Сигнатура FLAC отсутствует");
  let offset = 4;
  let last = false;
  let blocks = 0;
  while (!last) {
    if (++blocks > 128) throw new ParseError("Слишком много FLAC metadata blocks");
    const header = new ByteView(await file.read(offset, 4));
    last = Boolean(header.u8(0) & 0x80);
    const type = header.u8(0) & 0x7f;
    const length = header.u24(1);
    offset += 4;
    const block = await file.read(offset, length);
    if (type === 0) parseStreamInfo(block, file.size, metadata);
    if (type === 4) applyTags(parseVorbisComments(block), metadata);
    if (type === 6) parsePicture(block, metadata);
    offset += length;
  }
  if (!metadata.codec) throw new ParseError("FLAC STREAMINFO отсутствует");
  metadata.container = "FLAC";
  metadata.lossless = true;
}

/** Декодирует упакованные sample rate, channels, bit depth и total samples STREAMINFO. */
function parseStreamInfo(bytes, fileSize, metadata) {
  if (bytes.length !== 34) throw new ParseError("Некорректный FLAC STREAMINFO");
  const view = new ByteView(bytes);
  const sampleRate = (view.u8(10) << 12) | (view.u8(11) << 4) | (view.u8(12) >> 4);
  const channels = ((view.u8(12) & 0x0e) >> 1) + 1;
  const bitsPerSample = (((view.u8(12) & 1) << 4) | (view.u8(13) >> 4)) + 1;
  const samples = (view.u8(13) & 0x0f) * 0x100000000 + view.u32(14);
  if (!sampleRate || !channels || !bitsPerSample) throw new ParseError("Некорректные параметры FLAC");
  metadata.codec = "FLAC";
  metadata.sampleRate = sampleRate;
  metadata.channels = channels;
  metadata.bitsPerSample = bitsPerSample;
  if (samples) metadata.duration = samples / sampleRate;
  if (metadata.duration) metadata.bitrate = Math.round(fileSize * 8 / metadata.duration);
}

/** Разбирает стандартный FLAC PICTURE block и передаёт бинарные данные общей проверке. */
export function parsePicture(bytes, metadata) {
  const view = new ByteView(bytes);
  let offset = 0;
  view.range(offset, 4); offset += 4; // picture type
  const mimeLength = view.u32(offset); offset += 4;
  view.range(offset, mimeLength); offset += mimeLength;
  const descriptionLength = view.u32(offset); offset += 4;
  const description = decodeUTF8(view.slice(offset, descriptionLength)); offset += descriptionLength;
  view.range(offset, 16); offset += 16; // width, height, depth, colors
  const dataLength = view.u32(offset); offset += 4;
  applyCover(view.slice(offset, dataLength), description, metadata);
}
