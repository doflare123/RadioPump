// @ts-check

/** Максимальный объём одного чтения: защищает вкладку от ошибочных размеров chunk. */
export const MAX_BLOCK_BYTES = 16 * 1024 * 1024;

/** Ошибка означает повреждённую структуру или попытку чтения за границей файла. */
export class ParseError extends Error {
  /** @param {string} message */
  constructor(message) {
    super(message);
    this.name = "ParseError";
  }
}

/**
 * Выполняет проверенные произвольные чтения из Blob, не загружая весь трек в память.
 */
export class BinaryFile {
  /** @param {Blob} blob */
  constructor(blob) {
    this.blob = blob;
    this.size = blob.size;
  }

  /**
   * Читает точный диапазон. Каждый offset/length проверяется до вызова Blob.slice.
   * @param {number} offset @param {number} length @param {number} [limit]
   */
  async read(offset, length, limit = MAX_BLOCK_BYTES) {
    assertRange(offset, length, this.size, limit);
    const bytes = new Uint8Array(await this.blob.slice(offset, offset + length).arrayBuffer());
    if (bytes.length !== length) throw new ParseError("Файл неожиданно закончился");
    return bytes;
  }

  /** Читает не больше count последних байт, что нужно для ID3v1 и APEv2 footer. */
  async tail(count) {
    const length = Math.min(this.size, count);
    return this.read(this.size - length, length, count);
  }
}

/**
 * Обёртка над Uint8Array для безопасного чтения чисел и строк из уже полученного блока.
 */
export class ByteView {
  /** @param {Uint8Array} bytes */
  constructor(bytes) {
    this.bytes = bytes;
    this.view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    this.length = bytes.length;
  }

  /** @param {number} offset @param {number} length */
  range(offset, length) {
    assertRange(offset, length, this.length, this.length);
  }

  /** @param {number} offset */
  u8(offset) { this.range(offset, 1); return this.view.getUint8(offset); }

  /** @param {number} offset @param {boolean} [little] */
  u16(offset, little = false) { this.range(offset, 2); return this.view.getUint16(offset, little); }

  /** @param {number} offset @param {boolean} [little] */
  u24(offset, little = false) {
    this.range(offset, 3);
    return little
      ? this.u8(offset) | (this.u8(offset + 1) << 8) | (this.u8(offset + 2) << 16)
      : (this.u8(offset) << 16) | (this.u8(offset + 1) << 8) | this.u8(offset + 2);
  }

  /** @param {number} offset @param {boolean} [little] */
  u32(offset, little = false) { this.range(offset, 4); return this.view.getUint32(offset, little); }

  /** Возвращает uint64 только пока значение безопасно представляется JavaScript number. */
  /** @param {number} offset @param {boolean} [little] */
  u64(offset, little = false) {
    this.range(offset, 8);
    const value = this.view.getBigUint64(offset, little);
    if (value > BigInt(Number.MAX_SAFE_INTEGER)) throw new ParseError("Слишком большое 64-битное значение");
    return Number(value);
  }

  /** @param {number} offset @param {boolean} [little] */
  f32(offset, little = false) { this.range(offset, 4); return this.view.getFloat32(offset, little); }

  /** @param {number} offset @param {boolean} [little] */
  f64(offset, little = false) { this.range(offset, 8); return this.view.getFloat64(offset, little); }

  /** @param {number} offset @param {number} length */
  slice(offset, length) { this.range(offset, length); return this.bytes.slice(offset, offset + length); }

  /** @param {number} offset @param {number} length */
  ascii(offset, length) { return String.fromCharCode(...this.slice(offset, length)); }
}

/** Проверяет целые границы и индивидуальный лимит чтения. */
export function assertRange(offset, length, total, limit = MAX_BLOCK_BYTES) {
  if (!Number.isSafeInteger(offset) || !Number.isSafeInteger(length) || offset < 0 || length < 0) {
    throw new ParseError("Некорректная граница блока");
  }
  if (length > limit) throw new ParseError("Блок метаданных превышает допустимый размер");
  if (offset > total || length > total - offset) throw new ParseError("Блок выходит за границу файла");
}

/** Декодирует 4-байтное synchsafe-число ID3, проверяя старший бит каждого байта. */
export function synchsafe32(bytes, offset = 0) {
  const view = new ByteView(bytes);
  view.range(offset, 4);
  const values = [view.u8(offset), view.u8(offset + 1), view.u8(offset + 2), view.u8(offset + 3)];
  if (values.some((value) => value & 0x80)) throw new ParseError("Некорректный synchsafe-размер ID3");
  return (values[0] << 21) | (values[1] << 14) | (values[2] << 7) | values[3];
}

/**
 * Читает EBML variable integer. Для ID первый маркер остаётся, для размера удаляется.
 * @param {Uint8Array} bytes @param {number} offset @param {boolean} [keepMarker]
 */
export function ebmlVint(bytes, offset, keepMarker = false) {
  if (offset >= bytes.length) throw new ParseError("EBML-число отсутствует");
  const first = bytes[offset];
  let length = 1;
  let marker = 0x80;
  while (length <= 8 && (first & marker) === 0) { length += 1; marker >>= 1; }
  if (length > 8 || offset + length > bytes.length) throw new ParseError("Некорректное EBML-число");
  let value = keepMarker ? first : first & (marker - 1);
  for (let index = 1; index < length; index += 1) value = value * 256 + bytes[offset + index];
  const unknown = !keepMarker && value === 2 ** (7 * length) - 1;
  return { value, length, unknown };
}

/** Соединяет ограниченное число сегментов в один массив для Ogg packets. */
export function concatBytes(parts, maximum = MAX_BLOCK_BYTES) {
  const length = parts.reduce((sum, part) => sum + part.length, 0);
  if (length > maximum) throw new ParseError("Составной блок метаданных слишком большой");
  const result = new Uint8Array(length);
  let offset = 0;
  for (const part of parts) { result.set(part, offset); offset += part.length; }
  return result;
}
