// Проверяет изолированные Compose-проекты: upload, перезапуск, offline backup,
// восстановление в пустые тома и непрерывный эфир. Рабочая библиотека не используется.
import { spawnSync } from 'node:child_process';
import { mkdtempSync, writeFileSync, readFileSync, mkdirSync, rmSync, openSync, writeSync, closeSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { randomBytes, createHash } from 'node:crypto';
import assert from 'node:assert/strict';
import { setTimeout as delay } from 'node:timers/promises';

const root = resolve(import.meta.dirname, '..');
const temp = mkdtempSync(join(tmpdir(), 'radiopump-release-'));
const project = `rpcheck${Date.now()}`;
const restored = `${project}restore`;
const proxy = `${project}proxy`;
let proxyCreated = false;
const password = randomBytes(24).toString('hex');
const secret = randomBytes(32).toString('hex');
const seconds = Number(process.env.RADIOPUMP_SOAK_SECONDS || 600);
assert(Number.isInteger(seconds) && seconds >= 10, 'RADIOPUMP_SOAK_SECONDS >= 10');
const reportDir = join(root, 'release-results', project);
mkdirSync(reportDir, { recursive: true });
const envFile = join(temp, 'test.env');
writeFileSync(envFile, `RADIOPUMP_ADMIN_PASSWORD=${password}\nRADIOPUMP_JWT_SECRET=${secret}\nRADIOPUMP_PORT=0\n`);
const env = { ...process.env, RADIOPUMP_ADMIN_NAME: 'admin', RADIOPUMP_ADMIN_PASSWORD: password,
  RADIOPUMP_JWT_SECRET: secret, RADIOPUMP_PORT: '0', RADIOPUMP_BIND: '127.0.0.1',
  RADIOPUMP_CONFIG_PATH: './config.example.yaml' };

// Команды Docker получают только синтетические данные; секреты не попадают в вывод.
function docker(...args) {
  const r = spawnSync('docker', args, { cwd: root, env, encoding: 'utf8', timeout: 300000, maxBuffer: 16 * 1024 * 1024 });
  if (r.error || r.status !== 0) throw new Error(`docker ${args[0]}: ${r.error || r.stderr}`);
  return r.stdout.trim();
}
function compose(p, ...args) {
  return docker('compose', '--env-file', envFile, '-f', 'docker-compose.yaml', '-p', p, ...args);
}
function url(p) { return `http://${compose(p, 'port', 'radio', '8080')}`; }
async function json(base, path, token, body) {
  const headers = token ? { Authorization: `Bearer ${token}` } : {};
  if (body && !(body instanceof FormData)) headers['Content-Type'] = 'application/json';
  const r = await fetch(base + path, { method: body ? 'POST' : 'GET', headers,
    body: body instanceof FormData ? body : body ? JSON.stringify(body) : undefined,
    signal: AbortSignal.timeout(15000) });
  assert(r.ok, `${path}: HTTP ${r.status}`);
  return r.json();
}
async function login(base) { return (await json(base, '/api/auth/login', '', { name: 'admin', password })).token; }
const hash = bytes => createHash('sha256').update(bytes).digest('hex');

// Пустая станция выходит из backoff до 30 с после upload; это не разрыв эфира.
async function waitPlaying(base) {
  const deadline = Date.now() + 45000;
  while (Date.now() < deadline) {
    const state = await json(base, '/api/radio');
    if (state.stations.some(s => s.id === 'Main' && s.status === 'playing')) return;
    await delay(1000);
  }
  throw new Error('Станция не начала эфир за 45 секунд');
}

// Один HTTP-поток остаётся открытым весь прогон; отсутствие байтов 20 с — ошибка.
async function stream(base, duration, output) {
  const controller = new AbortController();
  let completed = false;
  let bytes = 0;
  // Длинный прогон пишет MP3 на диск, не накапливая часы аудио в памяти Node.
  const fd = output ? openSync(output, 'w') : undefined;
  let stalled = false;
  let watchdog;
  const reset = () => { clearTimeout(watchdog); watchdog = setTimeout(() => { stalled = true; controller.abort(); }, 20000); };
  const end = setTimeout(() => { completed = true; controller.abort(); }, duration * 1000);
  reset();
  try {
    const response = await fetch(base + '/stream/Main', { signal: controller.signal });
    assert.equal(response.status, 200);
    for await (const chunk of response.body) {
      reset(); bytes += chunk.length;
      if (fd !== undefined) writeSync(fd, chunk);
    }
    if (!completed) throw new Error('Эфир завершился до конца прогона');
  } catch (err) { if (!completed || stalled) throw err; }
  finally { clearTimeout(end); clearTimeout(watchdog); if (fd !== undefined) closeSync(fd); }
  assert(bytes > duration * 8000, `Недостаточно MP3-данных: ${bytes}`);
  return bytes;
}

// Все тома имеют уникальный префикс; очистка касается только созданных тестом проектов.
try {
  console.log('Чистая установка:', project);
  compose(project, 'up', '-d', '--no-build', '--wait', '--wait-timeout', '90');
  let base = url(project);
  let token = await login(base);
  assert.equal((await json(base, '/api/tracks', token)).length, 0);
  docker('run', '--rm', '--mount', `type=bind,src=${temp},dst=/fixture`, '--entrypoint', 'ffmpeg',
    'radiopump:local', '-v', 'error', '-f', 'lavfi', '-i', 'color=c=blue:s=32x32', '-frames:v', '1', '-threads', '1', '/fixture/cover.png');
  const cover = readFileSync(join(temp, 'cover.png'));
  for (const format of ['wav', 'flac', 'mp3']) {
    docker('run', '--rm', '--mount', `type=bind,src=${temp},dst=/fixture`, '--entrypoint', 'ffmpeg',
      'radiopump:local', '-v', 'error', '-f', 'lavfi', '-i', 'sine=frequency=440:duration=4', '-y', `/fixture/tone.${format}`);
    const form = new FormData();
    form.set('title', `Release ${format}`); form.set('artist', 'Synthetic'); form.set('album', 'Release test');
    form.set('duration', '4'); form.set('tag_ids', '1');
    form.set('file', new Blob([readFileSync(join(temp, `tone.${format}`))]), `tone.${format}`);
    form.set('cover', new Blob([cover]), 'cover.png');
    await json(base, '/api/admin/tracks', token, form);
  }
  const before = await json(base, '/api/tracks', token);
  assert.equal(before.length, 3);
  const audioHashes = {};
  for (const track of before) {
    assert.equal(track.Tags.length, 1);
    const r = await fetch(base + '/' + track.Path, { signal: AbortSignal.timeout(10000) });
    assert(r.ok); audioHashes[track.ID] = hash(Buffer.from(await r.arrayBuffer()));
  }
  await waitPlaying(base);
  await stream(base, 15);
  compose(project, 'restart', 'radio');
  compose(project, 'up', '-d', '--wait', '--wait-timeout', '90');
  base = url(project); token = await login(base);
  assert.deepEqual(await json(base, '/api/tracks', token), before);
  compose(project, 'stop', 'radio');
  const source = compose(project, 'ps', '-aq', 'radio');
  docker('run', '--rm', '--user', '0:0', '--volumes-from', `${source}:ro`,
    '--mount', `type=bind,src=${temp},dst=/backup`, '--entrypoint', 'tar', 'radiopump:local',
    '-czf', '/backup/library.tar.gz', '-C', '/app', 'data', 'music');
  // create создаёт пустые тома, но recovery ещё не запускается.
  compose(restored, 'create', 'radio');
  const destination = compose(restored, 'ps', '-aq', 'radio');
  docker('run', '--rm', '--user', '0:0', '--volumes-from', destination,
    '--mount', `type=bind,src=${temp},dst=/backup,readonly`, '--entrypoint', 'tar', 'radiopump:local',
    '-xzf', '/backup/library.tar.gz', '-C', '/app');
  compose(restored, 'up', '-d', '--wait', '--wait-timeout', '90');
  base = url(restored); token = await login(base);
  assert.deepEqual(await json(base, '/api/tracks', token), before);
  for (const track of before) {
    const r = await fetch(base + '/' + track.Path, { signal: AbortSignal.timeout(10000) });
    assert(r.ok); assert.equal(hash(Buffer.from(await r.arrayBuffer())), audioHashes[track.ID]);
    const image = await fetch(base + track.CoverURL, { signal: AbortSignal.timeout(10000) });
    assert(image.ok); assert.equal(hash(Buffer.from(await image.arrayBuffer())), hash(cover));
  }
  console.log(`Восстановление проверено. Непрерывный эфир: ${seconds} секунд.`);
  await waitPlaying(base);
  const sample = join(temp, 'stream.mp3');
  const bytes = await stream(base, seconds, sample);
  docker('run', '--rm', '--mount', `type=bind,src=${temp},dst=/check,readonly`, '--entrypoint', 'ffmpeg',
    'radiopump:local', '-v', 'error', '-xerror', '-i', '/check/stream.mp3', '-f', 'null', '-');
  const diagnostics = await json(base, '/api/admin/radio', token);
  assert(diagnostics.every(s => s.failures.length === 0), 'Карантин после прогона');
  // Локальный HTTP проверяет реальный proxy-поток без выдачи публичного сертификата.
  writeFileSync(join(temp, 'Caddyfile'), readFileSync(join(root, 'deploy', 'Caddyfile'), 'utf8').replace('radio.example.com {', ':8080 {'));
  docker('run', '-d', '--name', proxy, '--network', `${restored}_default`, '-p', '127.0.0.1::8080',
    '--mount', `type=bind,src=${join(temp, 'Caddyfile')},dst=/etc/caddy/Caddyfile,readonly`, 'caddy:2-alpine');
  proxyCreated = true;
  const proxyBase = `http://${docker('port', proxy, '8080')}`;
  let proxyReady = false;
  for (let attempt = 0; attempt < 20; attempt++) {
    try { await json(proxyBase, '/api/healthz'); proxyReady = true; break; } catch { await delay(500); }
  }
  assert(proxyReady, 'Reverse proxy недоступен');
  await stream(proxyBase, 15);
  writeFileSync(join(reportDir, 'result.json'), JSON.stringify({ seconds, bytes, tracks: before.length, formats: ['wav', 'flac', 'mp3'], restore: true, covers: true, proxy: true, diagnostics }, null, 2));
  console.log('PASS:', reportDir);
} finally {
  if (proxyCreated) {
    try { writeFileSync(join(reportDir, 'proxy.log'), docker('logs', proxy)); }
    finally { docker('rm', '-f', proxy); }
  }
  for (const p of [project, restored]) {
    try { writeFileSync(join(reportDir, `${p}.log`), compose(p, 'logs', '--no-color')); } catch {}
    compose(p, 'down', '-v', '--remove-orphans');
  }
  rmSync(temp, { recursive: true, force: true });
}
