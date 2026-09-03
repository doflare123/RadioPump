// @ts-check

// Связывает file input с локальным Worker и полями формы. До отдельного submit
// сетевых запросов нет; возвращаем контроллер для координации с upload-кодом.
/** @param {HTMLFormElement} form */
export function setupMetadataPreview(form) {
  const fileInput = form.elements.file;
  const submit = form.querySelector('[type="submit"]');
  const status = form.querySelector("[data-metadata-status]");
  const image = form.querySelector("[data-metadata-cover]");
  const details = form.querySelector("[data-metadata-details]");
  const fields = ["title", "artist", "album", "duration"];
  let worker = null;
  let timer = null;
  let imageURL = null;
  let parsing = false;
  let uploading = false;
  let valid = false;
  let version = 0;

  // Завершает текущий разбор и таймер; нужен при смене файла и уходе со страницы.
  function stop() {
    worker?.terminate();
    worker = null;
    clearTimeout(timer);
    timer = null;
    parsing = false;
  }
  // Удаляет старые сведения и освобождает память временного URL обложки.
  function clearPreview() {
    if (imageURL) URL.revokeObjectURL(imageURL);
    imageURL = null;
    image.removeAttribute("src");
    image.hidden = true;
    details.replaceChildren();
  }
  // Не позволяет редактировать поля во время разбора и отправлять непроверенный файл.
  function syncControls() {
    fileInput.disabled = uploading;
    fields.forEach((name) => { form.elements[name].disabled = parsing || uploading; });
    submit.disabled = parsing || uploading || !valid;
  }
  // Строит дополнительные сведения безопасными DOM-операциями и показывает обложку.
  function showDetails(metadata) {
    const rows = [
      ["Формат", `${metadata.container} · ${metadata.codec}`],
      ["Исполнитель альбома", metadata.albumArtist], ["Дата", metadata.date],
      ["Жанр", metadata.genre], ["Трек", metadata.trackNumber], ["Диск", metadata.discNumber],
      ["Композитор", metadata.composer], ["Комментарий", metadata.comment],
      ["Частота", metadata.sampleRate ? `${metadata.sampleRate} Гц` : ""],
      ["Каналы", metadata.channels],
      ["Битрейт", metadata.bitrate ? `${Math.round(metadata.bitrate / 1000)} кбит/с` : ""],
      ["Разрядность", metadata.bitsPerSample ? `${metadata.bitsPerSample} бит` : ""],
    ];
    for (const [label, value] of rows) {
      if (!value) continue;
      const dt = document.createElement("dt");
      const dd = document.createElement("dd");
      dt.textContent = label;
      dd.textContent = value;
      details.append(dt, dd);
    }
    if (metadata.cover) {
      imageURL = URL.createObjectURL(new Blob([metadata.cover.data], { type: metadata.cover.mimeType }));
      image.src = imageURL;
      image.hidden = false;
      image.onerror = () => {
        image.hidden = true;
        if (imageURL) URL.revokeObjectURL(imageURL);
        imageURL = null;
      };
    }
  }
  // Переводит форму в состояние ошибки: Worker остановлен, upload заблокирован.
  function fail(message) {
    stop();
    valid = false;
    status.textContent = message;
    syncControls();
  }
  // Каждый новый File отменяет предыдущую работу и запускает независимый Worker.
  fileInput.addEventListener("change", () => {
    version += 1;
    const current = version;
    stop();
    clearPreview();
    valid = false;
    fields.forEach((name) => { form.elements[name].value = ""; });
    const file = fileInput.files[0];
    if (!file) { status.textContent = ""; syncControls(); return; }
    parsing = true;
    status.textContent = "Читаем теги на вашем устройстве…";
    syncControls();
    try {
      worker = new Worker(new URL("./metadataWorker.js", import.meta.url), { type: "module" });
      // Принимаем только ответ текущей версии, чтобы старый File не перезаписал форму.
      worker.onmessage = ({ data }) => {
        if (version !== current) return;
        if (data.error) { fail(data.error); return; }
        stop();
        valid = true;
        const metadata = data.metadata;
        fields.forEach((name) => {
          form.elements[name].value = name === "duration" ? (metadata.duration ? Math.max(1, Math.round(metadata.duration)) : "") : metadata[name] || "";
        });
        showDetails(metadata);
        status.textContent = metadata.warnings.length
          ? `Файл прочитан с замечаниями: ${metadata.warnings.join("; ")}. Проверьте поля перед загрузкой.`
          : "Теги прочитаны локально. Проверьте и исправьте поля перед загрузкой.";
        syncControls();
      };
      worker.onerror = () => { if (version === current) fail("Не удалось запустить разбор файла. Обновите страницу и попробуйте ещё раз."); };
      timer = setTimeout(() => { if (version === current) fail("Разбор занял больше 30 секунд. Попробуйте другой файл."); }, 30_000);
      worker.postMessage(file);
    } catch { fail("Этот браузер не смог запустить локальный разбор файла"); }
  });
  // Сбрасывает служебное состояние вместе с нативным reset формы или pagehide.
  const reset = () => {
    version += 1;
    stop();
    clearPreview();
    valid = false;
    status.textContent = "";
    syncControls();
  };
  form.addEventListener("reset", reset);
  window.addEventListener("pagehide", reset);
  syncControls();
  return {
    canUpload: () => valid && !parsing && !uploading,
    setUploading(value) { uploading = value; syncControls(); },
  };
}
