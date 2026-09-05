# Open-Source Self-Hosted Radio (Written in Go)

[English](#english) | [Русский](#русский)

> Current implementation and operational limits: [File safety and streaming](docs/reliability.md).
> Реализованное поведение, проверки и ограничения: [Безопасные файлы и эфир](docs/reliability.md).

---

## English

A lightweight, highly customizable, open-source audio streaming and radio server designed to run on your own hardware. Load your tracks, tag them, and start your own 24/7 non-stop broadcast.

### ✨ Features

*   **Self-Hosted & Open-Source:** Complete control over your data and code. Deploy it anywhere.
*   **Smart Tagging:** Assign custom tags to your audio files based on mood, genre, or era.
*   **Main Wave:** A continuous 24/7 stream that mixes and plays absolutely every track in your library.
*   **Custom Waves:** Create separate, dedicated radio stations based on specific tags. Easily configured via config files (YAML) before building or launching.
*   **Powered by Go:** High performance, minimal memory footprint, and simple deployment with a single binary.

### 🚀 How It Works

1. Upload music through the admin page; automatic folder scanning is not implemented yet.
2. Assign tags to your songs (optional).
3. The server generates queues and streams MP3 at `/stream/{wave}`. Gapless transitions are not guaranteed yet; the home page plays the live stations with five-track history/queue and uploaded covers. See [Live radio page](docs/radio-page.md).

### 🛠 Wave Configuration (Example `config.yaml`)

You can flexibly configure additional streams through the configuration file:

```yaml
waves:
  - name: "Main Wave"
    tags: ["all"]
  - name: "Lo-Fi Chill"
    tags: ["lo-fi", "relax"]
  - name: "Hard Rock"
    tags: ["rock", "metal"]
```

### 📜 License

This project is open-source but **non-commercial**. You are free to download, study, run, and modify this code for your personal needs, but you are strictly prohibited from selling it or using it for commercial purposes.

This project is licensed under the **Creative Commons Attribution-NonCommercial 4.0 International (CC BY-NC 4.0)** License.

> **NonCommercial** — You may not use the material for commercial purposes.

You can read the summary of the license terms in the [LICENSE](LICENSE) file or visit the [official Creative Commons website](https://creativecommons.org).

---

## Русский

Легковесный и кастомизируемый open-source аудиоплеер/радиосервер для развертывания на собственной машине. Загрузите свои треки, настройте теги и запустите собственное вещание нон-стоп.

### ✨ Особенности проекта

*   **Self-Hosted & Open-Source:** Полный контроль над вашими данными и кодом. Разворачивайте где угодно.
*   **Умное тегирование:** Присваивайте трекам теги по настроению, жанрам или эпохам прямо в сервисе.
*   **Main Wave (Главная волна):** Потоковое вещание абсолютно всех ваших треков в режиме 24/7.
*   **Custom Waves (Тематические волны):** Создание отдельных радио-потоков на основе конкретных тегов. Настраивается через конфигурационные файлы (YAML) перед сборкой или запуском.
*   **Написано на Go:** Высокая производительность, низкое потребление оперативной памяти и простая сборка в один бинарник.

### 🚀 Как это работает

1. Загрузите музыку через административную страницу; автоматическое сканирование папки пока не реализовано.
2. Задаете теги для песен (опционально).
3. Сервер формирует очереди и транслирует MP3 через `/stream/{wave}`. Бесшовные переходы пока не гарантируются; главная воспроизводит живые станции с историей/очередью по пять треков и обложками новых загрузок. Подробнее: [Страница радио](docs/radio-page.md).

### 🛠 Конфигурация волн (Пример `config.yaml`)

Вы можете гибко настраивать дополнительные волны через конфигурационный файл:

```yaml
waves:
  - name: "Main Wave"
    tags: ["all"]
  - name: "Lo-Fi Chill"
    tags: ["lo-fi", "relax"]
  - name: "Hard Rock"
    tags: ["rock", "metal"]
```

### 📜 Лицензия

Этот проект является открытым, но **некоммерческим**. Вы можете свободно скачивать, изучать, запускать и модифицировать этот код под свои нужды, но не имеете права продавать его или использовать в коммерческих целях.

Проект лицензирован под международной лицензией **Creative Commons Attribution-NonCommercial 4.0 International (CC BY-NC 4.0)**.

> **NonCommercial** — You may not use the material for commercial purposes.
> *(Некоммерческое использование — Вы не имеете права использовать этот материал в коммерческих целях).*

Ознакомиться с кратким описанием условий можно в файле [LICENSE](LICENSE) или на [официальном сайте Creative Commons](https://creativecommons.org).
