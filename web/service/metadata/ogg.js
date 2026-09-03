// @ts-check

import { BinaryFile, ByteView, concatBytes, ParseError } from "./binaryReader.js";
import { parsePicture } from "./flac.js";
import { applyTags, parseVorbisComments } from "./textDecoder.js";

/**
 * Собирает первые Ogg packets из page/lacing-сегментов и разбирает Vorbis,
 * Opus или Speex headers/comments. Последнюю granule position ищет в хвосте.
 * @param {BinaryFile} file @param {import('./reader.js').TrackMetadata} metadata
 */
export async function parseOgg(file, metadata) {
  let offset = 0;
  let pageCount = 0;
  /** @type {Uint8Array[]} */
  let packetParts = [];
  let packetBytes = 0;
  /** @type {Uint8Array[]} */
  const packets = [];
  while (offset < file.size && packets.length < 3) {
    if (++pageCount > 256) throw new ParseError("Заголовки Ogg не найдены в первых страницах");
    const header = new ByteView(await file.read(offset, 27));
    if (header.ascii(0, 4) !== "OggS" || header.u8(4) !== 0) throw new ParseError("Некорректная страница Ogg");
    const segmentCount = header.u8(26);
    const lacing = await file.read(offset + 27, segmentCount);
    const bodyLength = lacing.reduce((sum, size) => sum + size, 0);
    const body = await file.read(offset + 27 + segmentCount, bodyLength);
    let bodyOffset = 0;
    for (const size of lacing) {
      packetParts.push(body.slice(bodyOffset, bodyOffset + size));
      packetBytes += size;
      if (packetBytes > 16 * 1024 * 1024) throw new ParseError("Ogg metadata packet слишком большой");
      bodyOffset += size;
      if (size < 255) {
        packets.push(concatBytes(packetParts));
        packetParts = [];
        packetBytes = 0;
        if (packets.length >= 3) break;
      }
    }
    offset += 27 + segmentCount + bodyLength;
  }
  if (!packets.length) throw new ParseError("Ogg identification packet отсутствует");
  let granuleRate = 0;
  if (starts(packets[0], [1, ...asciiBytes("vorbis")])) {
    granuleRate = parseVorbis(packets, metadata);
  } else if (starts(packets[0], asciiBytes("OpusHead"))) {
    granuleRate = parseOpus(packets, metadata);
  } else if (starts(packets[0], asciiBytes("Speex   "))) {
    granuleRate = parseSpeex(packets, metadata);
  } else {
    throw new ParseError("Кодек Ogg не поддерживается");
  }
  if (granuleRate) {
    const granule = await lastGranule(file);
    if (granule > 0) metadata.duration = granule / granuleRate;
  }
  if (metadata.duration) metadata.bitrate = Math.round(file.size * 8 / metadata.duration);
  metadata.container = "Ogg";
}

/** Читает identification/comment headers Ogg Vorbis. */
function parseVorbis(packets, metadata) {
  const first = new ByteView(packets[0]);
  if (first.length < 30) throw new ParseError("Обрезанный Vorbis header");
  metadata.codec = "Vorbis";
  metadata.channels = first.u8(11);
  metadata.sampleRate = first.u32(12, true);
  metadata.lossless = false;
  const comment = packets.find((packet) => starts(packet, [3, ...asciiBytes("vorbis")]));
  if (comment) applyCommentPacket(comment.slice(7), metadata);
  return metadata.sampleRate;
}

/** Читает OpusHead и OpusTags; granule Opus всегда измеряется в 48 kHz. */
function parseOpus(packets, metadata) {
  const first = new ByteView(packets[0]);
  if (first.length < 19) throw new ParseError("Обрезанный OpusHead");
  metadata.codec = "Opus";
  metadata.channels = first.u8(9);
  metadata.sampleRate = 48000;
  metadata.lossless = false;
  const comment = packets.find((packet) => starts(packet, asciiBytes("OpusTags")));
  if (comment) applyCommentPacket(comment.slice(8), metadata);
  return 48000;
}

/** Читает фиксированный Speex header и следующий Vorbis-style comment packet. */
function parseSpeex(packets, metadata) {
  const first = new ByteView(packets[0]);
  if (first.length < 80) throw new ParseError("Обрезанный Speex header");
  metadata.codec = "Speex";
  metadata.sampleRate = first.u32(36, true);
  metadata.channels = first.u32(48, true);
  metadata.bitrate = first.u32(52, true);
  metadata.lossless = false;
  if (packets[1]) applyCommentPacket(packets[1], metadata);
  return metadata.sampleRate;
}

/** Применяет comments и декодирует FLAC-picture, переданный в Base64-теге. */
function applyCommentPacket(bytes, metadata) {
  const tags = parseVorbisComments(bytes);
  applyTags(tags, metadata);
  const encoded = tags.metadatablockpicture?.[0];
  if (!encoded || encoded.length > 16 * 1024 * 1024) return;
  try {
    const binary = atob(encoded.replace(/\s/g, ""));
    const picture = Uint8Array.from(binary, (character) => character.charCodeAt(0));
    parsePicture(picture, metadata);
  } catch {
    metadata.warnings.push("Встроенная Ogg-обложка повреждена");
  }
}

/** Ищет последнюю полную OggS-страницу в хвосте и возвращает её uint64 granule. */
async function lastGranule(file) {
  const bytes = await file.tail(Math.min(file.size, 256 * 1024));
  for (let offset = bytes.length - 27; offset >= 0; offset -= 1) {
    if (!starts(bytes.slice(offset), asciiBytes("OggS"))) continue;
    try { return new ByteView(bytes).u64(offset + 6, true); } catch { return 0; }
  }
  return 0;
}

/** Сравнивает короткую сигнатуру без создания строк из бинарного потока. */
function starts(bytes, expected) {
  return expected.length <= bytes.length && expected.every((value, index) => bytes[index] === value);
}

/** Преобразует ASCII literal в числа для сигнатур Ogg packets. */
function asciiBytes(value) { return [...value].map((character) => character.charCodeAt(0)); }
