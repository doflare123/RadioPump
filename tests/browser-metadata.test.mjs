import test, { after } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Reader } from "../web/service/metadata/reader.js";
import { BinaryFile } from "../web/service/metadata/binaryReader.js";

const dir = mkdtempSync(join(tmpdir(), "radiopump-metadata-"));
after(() => rmSync(dir, { recursive: true, force: true }));

const formats = [
  ["mp3", "libmp3lame"], ["mp2", "mp2"], ["flac", "flac"], ["wav", "pcm_s16le"],
  ["ogg", "libvorbis"], ["opus", "libopus"], ["spx", "libspeex"], ["m4a", "aac"],
  ["aac", "aac"], ["aiff", "pcm_s16be"], ["wma", "wmav2"], ["wv", "wavpack"],
  ["mka", "flac"], ["webm", "libopus"],
];

for (const [extension, codec] of formats) {
  test(`reads tags and audio properties from .${extension}`, async () => {
    const path = join(dir, `track.${extension}`);
    const args = [
      "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=0.4",
      "-c:a", codec, "-metadata", "title=Parser title", "-metadata", "artist=Parser artist",
      "-metadata", "album=Parser album", "-metadata", "genre=Parser genre", "-y", path,
    ];
    if (extension === "aac") args.splice(args.length - 2, 0, "-f", "adts");
    execFileSync("ffmpeg", args);
    const bytes = readFileSync(path);
    const file = new File([bytes], `local.${extension}`, { type: "application/octet-stream" });
    const metadata = await Reader(file);
    const preservesTitle = !["mp2", "aac"].includes(extension);
    const preservesArtist = !["mp2", "aac", "aiff"].includes(extension);
    assert.equal(metadata.title, preservesTitle ? "Parser title" : "local");
    assert.equal(metadata.artist, preservesArtist ? "Parser artist" : "");
    // Raw formats do not necessarily carry container metadata.
    if (["mp3", "flac", "ogg", "opus", "spx", "m4a", "wma", "wv"].includes(extension)) {
      assert.equal(metadata.album, "Parser album");
    }
    assert.ok(metadata.duration > 0);
    assert.ok(metadata.codec);
    if (extension !== "wma") assert.ok(metadata.sampleRate > 0);
  });
}

test("uses the local filename when title is absent", async () => {
  const path = join(dir, "untagged.flac");
  execFileSync("ffmpeg", ["-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=duration=0.2", "-c:a", "flac", "-y", path]);
  const file = new File([readFileSync(path)], "Fallback name.flac");
  assert.equal((await Reader(file)).title, "Fallback name");
});

test("filters harmless ID3 padding diagnostics", async () => {
  const path = join(dir, "padding.mp3");
  execFileSync("ffmpeg", ["-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=duration=0.3", "-metadata", "title=Padding", "-c:a", "libmp3lame", "-y", path]);
  const metadata = await Reader(new File([readFileSync(path)], "padding.mp3"));
  assert.deepEqual(metadata.warnings, []);
});

test("extracts an embedded cover without uploading the file", async () => {
  const audio = join(dir, "covered.mp3");
  const cover = join(dir, "cover.png");
  writeFileSync(cover, Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=", "base64"));
  execFileSync("ffmpeg", [
    "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=duration=0.2", "-i", cover,
    "-map", "0:a", "-map", "1:v", "-c:a", "libmp3lame", "-c:v", "copy", "-id3v2_version", "3",
    "-metadata:s:v", "title=Album cover", "-metadata:s:v", "comment=Cover (front)", "-y", audio,
  ]);
  const metadata = await Reader(new File([readFileSync(audio)], "covered.mp3"));
  assert.equal(metadata.hasCover, true);
  assert.equal(metadata.cover.mimeType, "image/png");
  assert.ok(metadata.cover.data.length > 40);
});

/** MP4 cover хранится в ilst/covr и не должен считаться обычной видеодорожкой. */
test("extracts cover metadata from M4A", async () => {
  const audio = join(dir, "covered.m4a");
  const cover = join(dir, "m4a-cover.png");
  writeFileSync(cover, Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=", "base64"));
  execFileSync("ffmpeg", [
    "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=duration=0.2", "-i", cover,
    "-map", "0:a", "-map", "1:v", "-c:a", "aac", "-c:v", "copy", "-disposition:v", "attached_pic",
    "-metadata", "title=M4A cover", "-y", audio,
  ]);
  const metadata = await Reader(new File([readFileSync(audio)], "covered.m4a"));
  assert.equal(metadata.title, "M4A cover");
  assert.equal(metadata.cover?.mimeType, "image/png");
});

test("detects ALAC inside M4A and the accepted .alac alias", async () => {
  const path = join(dir, "lossless.m4a");
  execFileSync("ffmpeg", ["-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=duration=0.2", "-c:a", "alac", "-y", path]);
  const bytes = readFileSync(path);
  for (const name of ["lossless.m4a", "lossless.alac"]) {
    const metadata = await Reader(new File([bytes], name));
    assert.match(metadata.codec, /alac/i);
    assert.equal(metadata.lossless, true);
  }
});

test("rejects extension/content mismatch", async () => {
  const path = join(dir, "mismatch.flac");
  execFileSync("ffmpeg", ["-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=duration=0.2", "-c:a", "flac", "-y", path]);
  const file = new File([readFileSync(path)], "not-really.mp3");
  await assert.rejects(() => Reader(file), /не соответствует/);
});

test("rejects empty and unknown files", async () => {
  await assert.rejects(() => Reader(new File([], "empty.mp3")), /пуст/);
  await assert.rejects(() => Reader(new File(["x"], "track.exe")), /расширение/);
});

/** Кодирует uint32 в выбранном byte order для компактных самодельных fixtures. */
function u32(value, little = false) {
  const buffer = Buffer.alloc(4);
  little ? buffer.writeUInt32LE(value) : buffer.writeUInt32BE(value);
  return buffer;
}

/** Кодирует uint64 через BigInt, не теряя точность в DSD container headers. */
function u64(value, little = false) {
  const buffer = Buffer.alloc(8);
  little ? buffer.writeBigUInt64LE(BigInt(value)) : buffer.writeBigUInt64BE(BigInt(value));
  return buffer;
}

/** Создаёт synchsafe размер для ID3v2.4/header всех версий. */
function syncsafe(value) {
  return Buffer.from([(value >>> 21) & 0x7f, (value >>> 14) & 0x7f, (value >>> 7) & 0x7f, value & 0x7f]);
}

/** Собирает текстовый ID3 frame требуемой версии и кодировки. */
function id3Frame(version, id, encoding, value) {
  const text = encoding === 0 ? Buffer.from(value, "latin1") : encoding === 1
    ? Buffer.concat([Buffer.from([0xff, 0xfe]), Buffer.from(value, "utf16le")])
    : Buffer.from(value, "utf8");
  const payload = Buffer.concat([Buffer.from([encoding]), text]);
  const size = version === 2 ? Buffer.from([(payload.length >>> 16) & 255, (payload.length >>> 8) & 255, payload.length & 255]) : version === 4 ? syncsafe(payload.length) : u32(payload.length);
  return Buffer.concat([Buffer.from(id, "ascii"), size, version === 2 ? Buffer.alloc(0) : Buffer.alloc(2), payload]);
}

/** Проверяет ID3v2.2/2.3/2.4 и Latin-1/UTF-16/UTF-8 без внешнего генератора тегов. */
for (const fixture of [
  { version: 2, id: "TT2", encoding: 0, value: "Café" },
  { version: 3, id: "TIT2", encoding: 1, value: "Привет" },
  { version: 4, id: "TIT2", encoding: 3, value: "AC/DC" },
]) {
  test(`reads ID3v2.${fixture.version} text encoding ${fixture.encoding}`, async () => {
    const frame = id3Frame(fixture.version, fixture.id, fixture.encoding, fixture.value);
    const tag = Buffer.concat([Buffer.from("ID3", "ascii"), Buffer.from([fixture.version, 0, 0]), syncsafe(frame.length), frame]);
    const audio = Buffer.concat([tag, Buffer.from([0xff, 0xfb, 0x90, 0x64]), Buffer.alloc(4096)]);
    const metadata = await Reader(new File([audio], "id3.mp3"));
    assert.equal(metadata.title, fixture.value);
  });
}

/** Создаёт минимальный APEv2 footer с одним UTF-8 item. */
function withAPEv2(prefix, key, value) {
  const item = Buffer.concat([u32(Buffer.byteLength(value), true), u32(0, true), Buffer.from(`${key}\0`, "utf8"), Buffer.from(value, "utf8")]);
  const footer = Buffer.concat([Buffer.from("APETAGEX"), u32(2000, true), u32(item.length + 32, true), u32(1, true), u32(0, true), Buffer.alloc(8)]);
  return Buffer.concat([prefix, item, footer]);
}

/** Редкие APE/Musepack проверяются структурными fixtures, поскольку FFmpeg их не кодирует. */
for (const [name, magic, container] of [["rare.ape", "MAC ", "APE"], ["rare.mpc", "MPCK", "Musepack"]]) {
  test(`reads APEv2 from ${name}`, async () => {
    const metadata = await Reader(new File([withAPEv2(Buffer.from(magic), "Title", "Rare title")], name));
    assert.equal(metadata.title, "Rare title");
    assert.equal(metadata.container, container);
  });
}

/** Создаёт минимальный DSF с fmt chunk и ровно одной секундой DSD samples. */
function minimalDSF() {
  const format = Buffer.alloc(40);
  format.writeUInt32LE(1, 0); format.writeUInt32LE(0, 4); format.writeUInt32LE(1, 8);
  format.writeUInt32LE(2, 12); format.writeUInt32LE(2_822_400, 16); format.writeUInt32LE(1, 20);
  format.writeBigUInt64LE(2_822_400n, 24); format.writeUInt32LE(4096, 32);
  const fmt = Buffer.concat([Buffer.from("fmt "), u64(52, true), format]);
  return Buffer.concat([Buffer.from("DSD "), u64(28, true), u64(28 + fmt.length, true), u64(0, true), fmt]);
}

/** Создаёт минимальный DSDIFF с PROP/SND и небольшим DSD payload. */
function minimalDFF() {
  const fs = Buffer.concat([Buffer.from("FS  "), u64(4), u32(2_822_400)]);
  const channels = Buffer.concat([Buffer.from("CHNL"), u64(2), Buffer.from([0, 2])]);
  const propertyData = Buffer.concat([Buffer.from("SND "), fs, channels]);
  const property = Buffer.concat([Buffer.from("PROP"), u64(propertyData.length), propertyData]);
  const audio = Buffer.concat([Buffer.from("DSD "), u64(8), Buffer.alloc(8)]);
  return Buffer.concat([Buffer.from("FRM8"), u64(property.length + audio.length + 4), Buffer.from("DSD "), property, audio]);
}

/** DSF и DSDIFF проверки подтверждают сигнатуры и технические поля без личной музыки. */
test("reads structural DSF and DSDIFF fixtures", async () => {
  const dsf = await Reader(new File([minimalDSF()], "sample.dsf"));
  assert.equal(dsf.duration, 1);
  assert.equal(dsf.sampleRate, 2_822_400);
  const dff = await Reader(new File([minimalDFF()], "sample.dff"));
  assert.equal(dff.codec, "DSD");
  assert.equal(dff.channels, 2);
});

/** BinaryFile должен отклонять отрицательные и выходящие за Blob границы чтения. */
test("bounds every binary read", async () => {
  const binary = new BinaryFile(new Blob([Buffer.alloc(8)]));
  await assert.rejects(() => binary.read(-1, 1), /границ/);
  await assert.rejects(() => binary.read(7, 2), /границ/);
  await assert.rejects(() => binary.read(0, 9, 8), /превышает/);
});

/** Проверяет browser Worker message contract без сети и отправки выбранного File. */
test("returns metadata through the module Worker contract", async () => {
  const frame = id3Frame(4, "TIT2", 3, "Worker title");
  const tag = Buffer.concat([Buffer.from("ID3"), Buffer.from([4, 0, 0]), syncsafe(frame.length), frame]);
  const file = new File([tag, Buffer.from([0xff, 0xfb, 0x90, 0x64]), Buffer.alloc(1024)], "worker.mp3");
  let response;
  globalThis.self = { postMessage(value) { response = value; } };
  await import(`../web/service/metadataWorker.js?test=${Date.now()}`);
  await globalThis.self.onmessage({ data: file });
  delete globalThis.self;
  assert.equal(response.metadata.title, "Worker title");
  assert.equal(response.error, undefined);
});
