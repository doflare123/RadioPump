// @ts-check

import { BinaryFile, ParseError } from "./binaryReader.js";
import { parseMP3, parseADTS } from "./id3.js";
import { parseFLAC } from "./flac.js";
import { parseOgg } from "./ogg.js";
import { parseMP4 } from "./mp4.js";
import { parseRIFF } from "./riff.js";
import { parseAIFF } from "./aiff.js";
import { parseASF } from "./asf.js";
import { parseAPE, parseMusepack, parseWavPack } from "./ape.js";
import { parseMatroska } from "./matroska.js";
import { parseDFF, parseDSF } from "./dsd.js";

/** @typedef {{mimeType:string,data:Uint8Array,description:string}} CoverData */
/**
 * @typedef {Object} TrackMetadata
 * @property {string} title
 * @property {string} artist
 * @property {string} album
 * @property {string} albumArtist
 * @property {string} date
 * @property {string} genre
 * @property {string} trackNumber
 * @property {string} discNumber
 * @property {string} composer
 * @property {string} comment
 * @property {number} duration
 * @property {number} sampleRate
 * @property {number} channels
 * @property {number} bitrate
 * @property {number} bitsPerSample
 * @property {string} codec
 * @property {string} container
 * @property {boolean} lossless
 * @property {boolean} hasCover
 * @property {CoverData|null} cover
 * @property {string[]} warnings
 */

/** Разрешённые расширения сгруппированы по реальному контейнеру, а не по codec. */
const EXTENSIONS = new Set(["mp3", "mp2", "mpa", "flac", "wav", "wave", "ogg", "oga", "opus", "spx", "m4a", "m4b", "mp4", "alac", "aac", "aif", "aiff", "aifc", "wma", "ape", "wv", "mka", "webm", "mpc", "dsf", "dff"]);
const MATCH = {
  mpeg: new Set(["mp3", "mp2", "mpa"]), adts: new Set(["aac"]), flac: new Set(["flac"]),
  riff: new Set(["wav", "wave"]), ogg: new Set(["ogg", "oga", "opus", "spx"]),
  mp4: new Set(["m4a", "m4b", "mp4", "alac"]), aiff: new Set(["aif", "aiff", "aifc"]),
  asf: new Set(["wma"]), ape: new Set(["ape"]), wavpack: new Set(["wv"]),
  matroska: new Set(["mka", "webm"]), musepack: new Set(["mpc"]), dsf: new Set(["dsf"]), dff: new Set(["dff"]),
};

/**
 * Главная точка browser parser: проверяет файл, определяет контейнер по magic bytes,
 * сверяет его с расширением и вызывает один независимый модуль.
 * @param {File} source
 * @returns {Promise<TrackMetadata>}
 */
export async function Reader(source) {
  if (!(source instanceof Blob)) throw new TypeError("Ожидался локальный File");
  if (!source.size) throw new ParseError("Файл пуст");
  if (source.size > 512 * 1024 * 1024) throw new ParseError("Файл больше допустимых 512 MiB для предварительного просмотра");
  const extension = fileExtension(source.name || "");
  if (!EXTENSIONS.has(extension)) throw new ParseError(`расширение .${extension || "?"} не поддерживается`);
  const file = new BinaryFile(source);
  const prefix = await file.read(0, Math.min(file.size, 64));
  const kind = detectKind(prefix, extension);
  if (!kind) throw new ParseError("Формат аудиофайла не распознан по содержимому");
  if (!MATCH[kind].has(extension)) throw new ParseError(`Содержимое файла не соответствует расширению .${extension}`);
  const metadata = emptyMetadata();
  const parsers = { mpeg: parseMP3, adts: parseADTS, flac: parseFLAC, riff: parseRIFF, ogg: parseOgg, mp4: parseMP4, aiff: parseAIFF, asf: parseASF, ape: parseAPE, wavpack: parseWavPack, matroska: parseMatroska, musepack: parseMusepack, dsf: parseDSF, dff: parseDFF };
  await parsers[kind](file, metadata);
  metadata.title ||= baseName(source.name || "track");
  normalizeNumbers(metadata);
  if (!metadata.duration) metadata.warnings.push("Длительность не найдена; проверьте её перед загрузкой");
  return metadata;
}

/** Создаёт объект фиксированной формы, удобный для Worker structured clone и JSDoc. */
function emptyMetadata() {
  return { title: "", artist: "", album: "", albumArtist: "", date: "", genre: "", trackNumber: "", discNumber: "", composer: "", comment: "", duration: 0, sampleRate: 0, channels: 0, bitrate: 0, bitsPerSample: 0, codec: "", container: "", lossless: false, hasCover: false, cover: null, warnings: [] };
}

/** Определяет контейнер по сигнатуре; расширение в этом решении ничего не доказывает. */
function detectKind(bytes, extension) {
  const ascii = (offset, length) => offset + length <= bytes.length ? String.fromCharCode(...bytes.slice(offset, offset + length)) : "";
  const starts = (...values) => values.every((value, index) => bytes[index] === value);
  if (ascii(0, 4) === "fLaC") return "flac";
  if (ascii(0, 4) === "OggS") return "ogg";
  if (["RIFF", "RF64"].includes(ascii(0, 4)) && ascii(8, 4) === "WAVE") return "riff";
  if (ascii(0, 4) === "FORM" && ["AIFF", "AIFC"].includes(ascii(8, 4))) return "aiff";
  if (ascii(4, 4) === "ftyp") return "mp4";
  if (starts(0x30, 0x26, 0xb2, 0x75, 0x8e, 0x66, 0xcf, 0x11)) return "asf";
  if (ascii(0, 4) === "MAC ") return "ape";
  if (ascii(0, 4) === "wvpk") return "wavpack";
  if (starts(0x1a, 0x45, 0xdf, 0xa3)) return "matroska";
  if (ascii(0, 4) === "MPCK" || ascii(0, 3) === "MP+") return "musepack";
  if (ascii(0, 4) === "DSD ") return "dsf";
  if (ascii(0, 4) === "FRM8" && ascii(12, 4) === "DSD ") return "dff";
  if (ascii(0, 3) === "ID3") return sniffAfterID3(bytes, extension);
  if (starts(0xff) && (bytes[1] & 0xf6) === 0xf0) return "adts";
  if (starts(0xff) && (bytes[1] & 0xe0) === 0xe0) return "mpeg";
  return "";
}

/** После небольшого ID3 отличает ADTS от MPEG; крупный ID3 в .aac решает расширение. */
function sniffAfterID3(bytes, extension) {
  if (bytes.length >= 10 && !(bytes[6] & 0x80) && !(bytes[7] & 0x80) && !(bytes[8] & 0x80) && !(bytes[9] & 0x80)) {
    const size = (bytes[6] << 21) | (bytes[7] << 14) | (bytes[8] << 7) | bytes[9];
    const offset = 10 + size;
    if (offset + 2 <= bytes.length && bytes[offset] === 0xff && (bytes[offset + 1] & 0xf6) === 0xf0) return "adts";
  }
  return extension === "aac" ? "adts" : "mpeg";
}

/** Отбрасывает NaN/Infinity/отрицательные числа, которые не должны попасть в форму. */
function normalizeNumbers(metadata) {
  for (const key of ["duration", "sampleRate", "channels", "bitrate", "bitsPerSample"]) {
    if (!Number.isFinite(metadata[key]) || metadata[key] < 0) metadata[key] = 0;
  }
}

/** Возвращает последнее расширение в нижнем регистре. */
function fileExtension(name) { const dot = name.lastIndexOf("."); return dot >= 0 ? name.slice(dot + 1).toLowerCase() : ""; }

/** Использует имя файла как честный fallback title, не выдумывая остальные теги. */
function baseName(name) { const dot = name.lastIndexOf("."); return (dot > 0 ? name.slice(0, dot) : name).trim() || "track"; }

export default Reader;
