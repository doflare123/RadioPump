// @ts-check

import { BinaryFile, ByteView, MAX_BLOCK_BYTES, ParseError } from "./binaryReader.js";
import { applyCover, applyTags, decodeUTF16LE } from "./textDecoder.js";

const HEADER = "3026b2758e66cf11a6d900aa0062ce6c";
const FILE_PROPERTIES = "a1dcab8c47a9cf118ee400c00c205365";
const CONTENT_DESCRIPTION = "3326b2758e66cf11a6d900aa0062ce6c";
const EXTENDED_DESCRIPTION = "40a4d0d207e3d21197f000a0c95ea850";
const STREAM_PROPERTIES = "9107dcb7b7a9cf118ee600c00c205365";
const AUDIO_MEDIA = "409e69f84d5bcf11a8fd00805f5c442b";

/** Разбирает ASF/WMA header objects; packet data после header не читается. */
export async function parseASF(file, metadata) {
  if (file.size < 30) throw new ParseError("Обрезанный ASF");
  const header = new ByteView(await file.read(0, 30));
  if (guid(header.bytes, 0) !== HEADER) throw new ParseError("Некорректный ASF");
  const headerSize = header.u64(16, true);
  const count = header.u32(24, true);
  if (headerSize > file.size || count > 10000) throw new ParseError("Некорректный ASF header");
  /** @type {Record<string, string[]>} */
  const tags = {};
  let offset = 30;
  for (let index = 0; index < count && offset + 24 <= headerSize; index += 1) {
    const object = new ByteView(await file.read(offset, 24));
    const id = guid(object.bytes, 0);
    const size = object.u64(16, true);
    if (size < 24 || size > headerSize - offset) throw new ParseError("ASF object выходит за границу header");
    const payloadSize = size - 24;
    if (payloadSize > MAX_BLOCK_BYTES && id !== STREAM_PROPERTIES && id !== FILE_PROPERTIES) throw new ParseError("ASF metadata object слишком большой");
    if (id === FILE_PROPERTIES) await parseFileProperties(file, offset + 24, payloadSize, metadata);
    else if (id === STREAM_PROPERTIES) await parseStreamProperties(file, offset + 24, payloadSize, metadata);
    else if (id === CONTENT_DESCRIPTION) await parseContentDescription(file, offset + 24, payloadSize, tags);
    else if (id === EXTENDED_DESCRIPTION) await parseExtendedDescription(file, offset + 24, payloadSize, tags, metadata);
    offset += size;
  }
  applyTags(tags, metadata);
  metadata.container = "ASF";
  metadata.codec ||= "Windows Media Audio";
}

/** Вычисляет продолжительность ASF с учётом preroll в миллисекундах. */
async function parseFileProperties(file, offset, size, metadata) {
  if (size < 80) return;
  const view = new ByteView(await file.read(offset, 80));
  const playDuration = view.u64(40, true) / 10_000_000;
  const preroll = view.u64(56, true) / 1000;
  metadata.duration ||= Math.max(0, playDuration - preroll);
  metadata.bitrate ||= view.u32(76, true);
}

/** Находит audio stream и читает вложенный WAVEFORMATEX. */
async function parseStreamProperties(file, offset, size, metadata) {
  if (size < 72) return;
  const view = new ByteView(await file.read(offset, Math.min(size, 96)));
  if (guid(view.bytes, 0) !== AUDIO_MEDIA) return;
  const typeLength = view.u32(40, true);
  if (typeLength < 16 || view.length < 54 + Math.min(typeLength, 18)) return;
  const base = 54;
  const codec = view.u16(base, true);
  metadata.channels = view.u16(base + 2, true);
  metadata.sampleRate = view.u32(base + 4, true);
  metadata.bitrate = view.u32(base + 8, true) * 8;
  metadata.bitsPerSample = view.u16(base + 14, true);
  const names = { 0x160: "WMA", 0x161: "WMA v2", 0x162: "WMA Pro", 0x163: "WMA Lossless" };
  metadata.codec = names[codec] || `WMA codec 0x${codec.toString(16)}`;
  metadata.lossless = codec === 0x163;
}

/** Читает пять стандартных UTF-16LE полей Content Description. */
async function parseContentDescription(file, offset, size, tags) {
  if (size < 10) return;
  const bytes = await file.read(offset, size);
  const view = new ByteView(bytes);
  const lengths = Array.from({ length: 5 }, (_, index) => view.u16(index * 2, true));
  const names = ["title", "artist", "copyright", "comment", "rating"];
  let cursor = 10;
  for (let index = 0; index < lengths.length; index += 1) {
    const length = lengths[index];
    if (length > bytes.length - cursor) throw new ParseError("Обрезанный ASF Content Description");
    const value = decodeUTF16LE(view.slice(cursor, length));
    if (value) (tags[names[index]] ||= []).push(value);
    cursor += length;
  }
}

/** Разбирает именованные descriptors, включая двоичные WM/Picture. */
async function parseExtendedDescription(file, offset, size, tags, metadata) {
  if (size < 2) return;
  const bytes = await file.read(offset, size);
  const view = new ByteView(bytes);
  const count = view.u16(0, true);
  if (count > 10000) throw new ParseError("Слишком много ASF descriptors");
  let cursor = 2;
  const mapping = { "wm/albumtitle": "album", "wm/albumartist": "albumartist", "wm/year": "date", "wm/genre": "genre", "wm/tracknumber": "tracknumber", "wm/partofset": "discnumber", "wm/composer": "composer", description: "comment" };
  for (let index = 0; index < count; index += 1) {
    if (cursor + 8 > bytes.length) throw new ParseError("Обрезанный ASF descriptor");
    const nameLength = view.u16(cursor, true); cursor += 2;
    const name = decodeUTF16LE(view.slice(cursor, nameLength)).toLowerCase(); cursor += nameLength;
    const type = view.u16(cursor, true); const valueLength = view.u16(cursor + 2, true); cursor += 4;
    if (valueLength > bytes.length - cursor) throw new ParseError("Значение ASF выходит за границу");
    const value = view.slice(cursor, valueLength); cursor += valueLength;
    if (name === "wm/picture" && type === 1) parsePicture(value, metadata);
    else if (mapping[name]) {
      const text = type === 0 ? decodeUTF16LE(value) : type === 3 && value.length >= 4 ? String(new ByteView(value).u32(0, true)) : "";
      if (text) (tags[mapping[name]] ||= []).push(text);
    }
  }
}

/** Извлекает бинарный WM/Picture после MIME и описания UTF-16LE. */
function parsePicture(bytes, metadata) {
  if (bytes.length < 9) return;
  const view = new ByteView(bytes);
  const dataLength = view.u32(1, true);
  let cursor = 5;
  for (let field = 0; field < 2; field += 1) {
    while (cursor + 1 < bytes.length && (bytes[cursor] !== 0 || bytes[cursor + 1] !== 0)) cursor += 2;
    cursor += 2;
  }
  if (dataLength <= bytes.length - cursor) applyCover(view.slice(cursor, dataLength), "", metadata);
}

/** Представляет GUID в байтовом порядке файла без строковых преобразований endianness. */
function guid(bytes, offset) {
  return Array.from(bytes.slice(offset, offset + 16), value => value.toString(16).padStart(2, "0")).join("");
}
