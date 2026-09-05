import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
const source = await readFile(new URL("../web/service/radioPlayer.js", import.meta.url), "utf8");
const { RadioPlayer } = await import(`data:text/javascript;base64,${Buffer.from(source).toString("base64")}`);

// Управляемые media events и таймеры проверяют гонки без реального ожидания сети.
class FakeAudio extends EventTarget {
  currentTime = 0;
  buffered = { length: 0 };
  paused = true;
  loads = 0;
  plays = [];
  load() { this.loads++; }
  play() { this.paused = false; return new Promise((resolve, reject) => this.plays.push({ resolve, reject })); }
  pause() { this.paused = true; this.dispatchEvent(new Event("pause")); }
  removeAttribute() { this.src = ""; }
}
function fixture() {
  let now = 0;
  const timers = { pending: new Map(), next: 1, setTimeout(fn) { const id = this.next++; this.pending.set(id, fn); return id; }, clearTimeout(id) { this.pending.delete(id); }, setInterval(fn) { this.tick = fn; return 1; }, clearInterval() {} };
  const audio = new FakeAudio();
  const player = new RadioPlayer(audio, "", () => {}, () => now, timers);
  return { audio, player, timers, advance(ms) { now += ms; timers.tick(); } };
}

test("old play rejection cannot restart newly selected station", async () => {
  const {audio,player,timers} = fixture();
  player.select("rock"); player.select("chill");
  const src = audio.src;
  audio.plays[0].reject(new Error("old connection"));
  await Promise.resolve();
  assert.equal(player.station,"chill"); assert.equal(audio.src,src); assert.equal(timers.pending.size,0);
  player.destroy();
});
test("one retry after repeated failures, manual stop cancels it", () => {
  const {audio,player,timers} = fixture();
  player.select("rock");
  audio.dispatchEvent(new Event("error")); audio.dispatchEvent(new Event("ended"));
  assert.equal(timers.pending.size,1);
  player.stop(); assert.equal(timers.pending.size,0); assert.equal(audio.src,"");
  player.destroy();
});
test("offline waits, online reconnects and watchdog recovers a silent stall", () => {
  const {player,timers,audio,advance} = fixture();
  player.select("rock"); player.setOnline(false);
  assert.equal(player.phase,"offline"); assert.equal(timers.pending.size,0);
  advance(25000); assert.equal(timers.pending.size,0);
  player.setOnline(true); assert.match(audio.src,/stream\/rock/);
  advance(21000); assert.equal(player.phase,"reconnecting"); assert.equal(timers.pending.size,1);
  player.destroy();
});
test("healthy media progress does not reconnect and external pause stops", () => {
  const {player,timers,audio,advance} = fixture();
  player.select("rock"); audio.dispatchEvent(new Event("playing"));
  const loads = audio.loads;
  for(let i=1;i<30;i++) { audio.currentTime=i; audio.dispatchEvent(new Event("timeupdate")); advance(1000); }
  assert.equal(audio.loads,loads); assert.equal(timers.pending.size,0);
  audio.pause(); assert.equal(player.station,null); advance(25000); assert.equal(timers.pending.size,0);
  player.destroy();
});

test("pause emitted before natural EOF still reconnects", () => {
  const {player,timers,audio} = fixture();
  player.select("rock"); audio.dispatchEvent(new Event("playing"));
  audio.ended = true; audio.pause(); audio.dispatchEvent(new Event("ended"));
  assert.equal(player.station,"rock"); assert.equal(player.phase,"reconnecting");
  assert.equal(timers.pending.size,1);
  player.destroy();
});
