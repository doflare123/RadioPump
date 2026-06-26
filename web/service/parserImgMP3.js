export function parseAPICFrame(arrayBuffer, dataOffset, frameSize) {
    const view = new DataView(arrayBuffer);
    const frameEnd = dataOffset + frameSize;
    let currentOffset = dataOffset;
    if (frameSize <= 0 || frameEnd > view.byteLength) return null;

    // 1. Байт кодировки текста описания
    const encoding = view.getUint8(currentOffset);
    currentOffset += 1;

    // 2. Читаем MIME-тип (строка ASCII, ищем нулевой байт 0x00)
    let mimeType = '';
    while (currentOffset < frameEnd) {
        const charCode = view.getUint8(currentOffset);
        currentOffset += 1;
        if (charCode === 0) break; // Конец строки MIME-типа
        mimeType += String.fromCharCode(charCode);
    }

    // 3. Тип картинки (1 байт)
    if (currentOffset >= frameEnd) return null;
    const pictureType = view.getUint8(currentOffset);
    currentOffset += 1;

    // 4. Пропускаем описание (Description). 
    // Нам оно обычно не нужно, просто ищем терминирующий нулевой байт.
    if (encoding === 1 || encoding === 2) {
        // UTF-16: ищем два нулевых байта (0x00 0x00) с выравниванием
        while (currentOffset + 1 < frameEnd) {
            if (view.getUint16(currentOffset) === 0) {
                currentOffset += 2;
                break;
            }
            currentOffset += 2;
        }
    } else {
        // UTF-8 / ISO-8859-1: ищем один нулевой байт (0x00)
        while (currentOffset < frameEnd) {
            const charCode = view.getUint8(currentOffset);
            currentOffset += 1;
            if (charCode === 0) break;
        }
    }

    // 5. Все оставшиеся байты фрейма — это сама картинка
    const imageSize = frameEnd - currentOffset;
    
    if (imageSize <= 0) return null;

    // Вырезаем чистый бинарный кусок картинки
    const imageBuffer = arrayBuffer.slice(currentOffset, currentOffset + imageSize);
    
    // Создаем Blob и генерируем URL
    const blob = new Blob([imageBuffer], { type: mimeType || 'image/jpeg' });
    const imageUrl = URL.createObjectURL(blob);

    return {
        mimeType,
        pictureType,
        url: imageUrl
    };
}
