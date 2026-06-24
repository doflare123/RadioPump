

async function readID3Header(file) {
      const headerBuffer = await file.slice(0, 10).arrayBuffer();
      const header = new Uint8Array(headerBuffer);

      if (
          header[0] !== 0x49 ||
          header[1] !== 0x44 ||
          header[2] !== 0x33
      ) {
          return null;
      }

      const version = header[3];
      const revision = header[4];
      const flags = header[5];

      const tagSize =
          ((header[6] & 0x7f) << 21) |
          ((header[7] & 0x7f) << 14) |
          ((header[8] & 0x7f) << 7) |
          (header[9] & 0x7f);

      return {
          version,
          revision,
          flags,
          tagStart: 10,
          tagEnd: 10 + tagSize,
          tagSize,
      };
}