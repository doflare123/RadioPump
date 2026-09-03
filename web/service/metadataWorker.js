// @ts-check

import { Reader } from "./metadata/reader.js";

// Worker получает локальный File, выполняет тяжёлый разбор вне UI-потока и
// возвращает клонируемый результат. Ошибку переводим в понятную строку.
self.onmessage = async ({ data: file }) => {
  try { self.postMessage({ metadata: await Reader(file) }); }
  catch (error) {
    const message = error instanceof Error ? error.message : "Не удалось прочитать метаданные";
    self.postMessage({ error: message });
  }
};
