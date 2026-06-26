import { parseAPICFrame } from "./parserImgMP3.js";


async function readID3Header(file) {
  const headerBuffer = await file.slice(0, 10).arrayBuffer();
  const header = new Uint8Array(headerBuffer);
  let imgInfo;

  if (
      header[0] !== 0x49 ||
      header[1] !== 0x44 ||
      header[2] !== 0x33
  ) {
      return null;
  }

  const tagSize =
      ((header[6] & 0x7f) << 21) |
      ((header[7] & 0x7f) << 14) |
      ((header[8] & 0x7f) << 7) |
      (header[9] & 0x7f);

  
  let tagStart = 10;
  let tagEnd = 10 + tagSize;

  let tagBuffer = await file.slice(tagStart, tagEnd).arrayBuffer();
  let view = new DataView(tagBuffer);
  let tags = {};
  let textDecoder = new TextDecoder('utf-8');

  const frameMap ={
      'TIT2': 'title',  // Название трека
      'TPE1': 'artist', // Исполнитель
      'TALB': 'album',  // Альбом
      'TYER': 'year'    // Год
  };

  let pointer = 0

  while(pointer + 10 <= view.byteLength){

    // Читаем ID фрейма (4 символа)
    let frameId = ''
    for(let i = 0; i < 4; i++){
      const charCode = view.getUint8(pointer+i);
      if(charCode == 0) break;
      frameId += String.fromCharCode(charCode);
    }
    
    // Если пошел паддинг (нулевые байты), останавливаемся
    if(!frameId || frameId.length < 4) break;
    
    // Читаем размер данных фрейма (4 байта)
    const frameSize = view.getUint32(pointer+4);

    // Смещение к самим данным фрейма (заголовок фрейма = 10 байт)
    const dataPointer = pointer + 10;

    if (frameId === "APIC") {
      imgInfo = parseAPICFrame(tagBuffer, dataPointer, frameSize);
    }

    if(frameMap[frameId]){
      // Первый байт текстовых фреймов — это кодировка (Encoding flag)
      // 0 = ISO-8859-1, 1 = UTF-16 с BOM, 3 = UTF-8
      const encoder = view.getUint8(dataPointer)

      // Берем срез данных (пропуская байт кодировки)
      const textBuffer = tagBuffer.slice(dataPointer + 1, dataPointer + frameSize)

      // Декодируем
      let text = textDecoder.decode(textBuffer).replace(/\0/g, '').trim();

      tags[frameMap[frameId]] = text;

       // Шагаем к следующему фрейму: текущий offset + 10 байт заголовка + размер данных
    }

    pointer += 10 + frameSize;
  }

  return { ...tags, imgInfo }
} 


async function Reader(file) {
    let exp;
    const headerBuffer = await file.slice(0, 12).arrayBuffer();
    const header = new Uint8Array(headerBuffer);

    if (
        header[0] === 0x49 &&
        header[1] === 0x44 &&
        header[2] === 0x33
    ) {
        exp = "mp3";
    }
    else if (
        (header[0] === 0xFF && (header[1] & 0xE0) === 0xE0) // Проверка фрейма MPEG (без тегов)
    ) {
        exp = "mp3-notags";
    }
    else if (
        header[0] === 0x52 &&
        header[1] === 0x49 &&
        header[2] === 0x46 &&
        header[3] === 0x46 &&
        header[8] === 0x57 && // 'W'
        header[9] === 0x41 && // 'A'
        header[10] === 0x56 && // 'V'
        header[11] === 0x45    // 'E'
    ) {
        exp = "wav"; // RIFF контейнер с WAVE внутри
    }
    else if (
        header[0] === 0x46 &&
        header[1] === 0x4c &&
        header[2] === 0x41 &&
        header[3] === 0x43
    ) {
        exp = "flac";
    }
    else if (
        header[4] === 0x66 && // Смещение 4!
        header[5] === 0x74 && // 't' -> 0x74
        header[6] === 0x79 && // 'y' -> 0x79
        header[7] === 0x70    // 'p' -> 0x70
    ) {
        exp = "m4a";
    }
    else if (
        header[0] === 0x4f &&
        header[1] === 0x67 &&
        header[2] === 0x67 &&
        header[3] === 0x53
    ) {
        exp = "ogg";
    }
    else if (
        header[0] === 0xFF &&
        (header[1] === 0xF1 || header[1] === 0xF9)
    ) {
        exp = "aac"; // Сырой поток AAC (ADTS фрейм)
    }
    else {
        return
    }


    let raw
    switch (exp) {
        case "mp3":
            raw = await readID3Header(file) 
            break;
    
        default:
            break;
    }


}
