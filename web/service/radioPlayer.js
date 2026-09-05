// @ts-check

// Единственный audio принадлежит контроллеру, а не карточкам. Переключение и
// восстановление создают новую попытку; поздний reject старого play игнорируется.
export class RadioPlayer {
  constructor(audio, base, onChange, clock = () => Date.now(), timers = globalThis) {
    this.audio = audio;
    this.base = base;
    this.onChange = onChange;
    this.clock = clock;
    this.timers = timers;
    this.station = null;
    this.phase = "stopped";
    this.attempt = 0;
    this.failures = 0;
    this.retry = null;
    this.lastTime = 0;
    this.lastProgress = clock();
    this.audibleOrigin = null;
    this.serverNow = () => Date.now();
    this.online = true;
    audio.preload = "none";
    for (const event of ["error", "ended"]) audio.addEventListener(event, () => this.recover());
    audio.addEventListener("waiting", () => {
      if (this.station && !this.retry) this.setPhase("buffering");
    });
    audio.addEventListener("playing", () => {
      if (!this.station || this.retry) return;
      // HTMLAudio не раскрывает точную серверную метку первого MP3 frame.
      // Оцениваем её по серверному времени и уже накопленному аудиобуферу.
      const end = audio.buffered.length ? audio.buffered.end(audio.buffered.length - 1) : audio.currentTime;
      this.audibleOrigin = this.serverNow() - end * 1000;
      this.lastProgress = this.clock();
      this.setPhase("playing");
    });
    audio.addEventListener("timeupdate", () => {
      if (audio.currentTime > this.lastTime) {
        this.lastTime = audio.currentTime;
        this.lastProgress = this.clock();
        if (audio.currentTime > 10) this.failures = 0;
      }
    });
    audio.addEventListener("pause", () => {
      // Внешняя пауза (например системное управление) — явная остановка,
      // поэтому watchdog не включает музыку против желания слушателя.
      if (this.station && this.phase === "playing" && audio.paused && !audio.ended && !audio.error) this.stop();
    });
    this.watchdog = timers.setInterval(() => {
      if (this.station && !this.retry && this.online && this.clock() - this.lastProgress > 20000) this.recover();
    }, 1000);
  }

  setPhase(phase) { this.phase = phase; this.onChange(); }

  // Кнопка остановки освобождает HTTP-подписку; следующее включение идёт в live.
  select(id) {
    if (this.station === id) { this.stop(); return; }
    this.stop();
    this.station = id;
    this.connect();
  }

  stop() {
    this.station = null;
    this.attempt++;
    this.timers.clearTimeout(this.retry);
    this.retry = null;
    this.failures = 0;
    this.audibleOrigin = null;
    this.setPhase("stopped");
    this.audio.pause();
    this.audio.removeAttribute("src");
    this.audio.load();
  }

  connect() {
    if (!this.station) return;
    if (!this.online) { this.setPhase("offline"); return; }
    const attempt = ++this.attempt;
    this.retry = null;
    this.lastTime = 0;
    this.lastProgress = this.clock();
    this.audibleOrigin = null;
    this.setPhase("connecting");
    this.audio.src = `${this.base}/stream/${encodeURIComponent(this.station)}?attempt=${attempt}`;
    this.audio.load();
    this.audio.play().catch((error) => {
      if (attempt !== this.attempt || !this.station) return;
      if (error.name === "NotAllowedError") {
        this.stop();
        this.setPhase("blocked");
      } else this.recover();
    });
  }

  // Один retry с ограниченным backoff и jitter; ошибка сведений эфира сюда не попадает.
  recover() {
    if (!this.station || this.retry) return;
    this.attempt++;
    this.setPhase(this.online ? "reconnecting" : "offline");
    this.audio.pause();
    this.audio.removeAttribute("src");
    this.audio.load();
    if (!this.online) return;
    const delay = Math.min(30000, 1000 * 2 ** Math.min(this.failures++, 5)) + Math.random() * 500;
    this.retry = this.timers.setTimeout(() => this.connect(), delay);
  }

  setOnline(online) {
    this.online = online;
    if (!this.station) return;
    this.timers.clearTimeout(this.retry);
    this.retry = null;
    if (online) this.connect(); else this.recover();
  }

  destroy() { this.stop(); this.timers.clearInterval(this.watchdog); }
}
