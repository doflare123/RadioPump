# RadioPump user guide

[Home](../README.en.md) · [Русский](guide.ru.md)

## First start

You need Docker with Compose; use Linux containers in Docker Desktop. Download and extract the project. Run all commands below from the folder containing `docker-compose.yaml`. Use PowerShell on Windows or a terminal on Linux and macOS.

1. Copy `.env.example` in the same folder and name the copy `.env` (not `.env.txt`).
2. Open `.env` in a text editor.
3. After `RADIOPUMP_ADMIN_PASSWORD=`, enter a unique password with at least 12 Latin letters, digits or symbols. Put the value in single quotes.
4. Generate a separate random key:

```sh
docker run --rm debian:bookworm-slim sh -c "od -An -N32 -tx1 /dev/urandom | tr -d ' \n'"
```

Copy the resulting 64-character string after `RADIOPUMP_JWT_SECRET=`. This key protects administrator sign-ins; you do not need to memorize it. Do not use your password as the key. Leave the other settings as they are for now. Keep `.env` private.

Start the radio:

```sh
docker compose up -d --build --wait
```

The first start downloads and builds the required programs. Open [http://localhost:8080/](http://localhost:8080/). The radio keeps running while the computer and Docker are on, even after you close the terminal.

## Adding music

The current web interface is in Russian. Button names below match the interface.

1. Open the [admin sign-in page](http://localhost:8080/admin-login.html). Use `admin` and the password you set in `.env`.
2. On the tracks page, choose a file under **«Файл»**. Wait for the title, artist, album and duration to appear.
3. Edit these fields if needed. If the file has no title, its filename is used.
4. Optionally select station tags, such as `rock` or `ambient`.
5. Click **«Загрузить»** (Upload). Until then, the file stays on your device.
6. Return to **«Главная»** (Home) and play the `Main` station.

Cover art found in the file is saved with the track. A record illustration appears when there is no cover. Extra details such as year and genre can be viewed before uploading, but are not saved in the library yet.

Accepted formats include MP3, MP2, FLAC, WAV, Ogg, Opus, M4A/MP4, AAC, AIFF, WMA, APE, WavPack, Musepack, MKA/WebM, DSF and DFF. A damaged file may upload but fail to play. The supplied settings allow up to 100 MB per file. Uploads must finish within 60 seconds.

Use the uploaded tracks list to edit fields and tags or delete a track. Deleting a track also removes its audio file from the server. Simply copying music into a folder does not add it to the radio — use the upload form.

## Stations and tags

A tag is your own label for choosing music. You can assign it to several tracks. Create labels under **«Справочник тегов станций»** (Station tag list) at the bottom of the admin page. Tags inside an audio file do not automatically assign the track to a station.

To add stations:

1. Copy `config.example.yaml` to `config.production.yaml`.
2. Add `RADIOPUMP_CONFIG_PATH=./config.production.yaml` to `.env`.
3. Replace the `waves` section in `config.production.yaml`:

```yaml
waves:
  - name: Main
    tags: []
  - name: Chill
    tags: [lo-fi, ambient]
  - name: Rock
    tags: [rock, metal]
```

`Main` plays the whole library. `Chill` plays tracks tagged `lo-fi` **or** `ambient`. `Rock` plays tracks tagged `rock` **or** `metal`. Give each station a different name. Create any custom tags in the admin page first and assign them to tracks. Keep the indentation shown in the example, using spaces.

Apply the settings:

```sh
docker compose up -d --force-recreate --wait
```

You cannot rename or delete a tag used in station settings. Remove it from the settings file first and run the command above.

## Other settings

Docker reads `config.example.yaml` by default, not `config.yaml`. Use your own copy for changes, as described above.

| Setting | Purpose |
| --- | --- |
| `.env`: `RADIOPUMP_ADMIN_NAME` | Administrator username. Default: `admin`. |
| `.env`: `RADIOPUMP_ADMIN_PASSWORD` | Administrator password. |
| `.env`: `RADIOPUMP_PORT` | Website port. If 8080 is taken, use 8081 and open `http://localhost:8081/`. |
| `config.production.yaml`: `music.max_file_size_mb` | Maximum file size in MB. The browser reads files up to 512 MB. |
| `config.production.yaml`: `stream.bitrate` | Audio quality and bandwidth use. Default: `128k`. |

Run the apply-settings command above after changes. In Docker, keep `server.port: 8080` and `music.dir: ./music`. You can leave the other parameters alone.

To change the password and end existing administrator sessions, replace both the password and the random key in `.env`, then apply the settings.

## Access from a phone or another computer

For devices on the same network, set this in `.env`:

```dotenv
RADIOPUMP_BIND=0.0.0.0
```

Apply the settings. Find the radio computer's local IP in its network settings. On another device, open an address such as `http://192.168.1.50:8080/`, using your own IP. If the page does not open, allow incoming connections to the chosen port in your firewall settings.

## Access over the internet

You need a server reachable from the internet and a domain name. The password protects library management; listening and original audio files are available without signing in.

1. Point the domain's A record to your server's IP. Only use an AAAA record if IPv6 works.
2. Open ports 80 and 443 on the server and, if needed, your router.
3. Replace `radio.example.com` with your domain in `deploy/Caddyfile`.
4. Keep `RADIOPUMP_BIND=127.0.0.1` in `.env`.
5. Run:

```sh
docker compose -f docker-compose.yaml -f compose.https.yaml up -d --build --wait
```

Caddy is the program that receives connections to your domain and provides secure HTTPS access. This command starts it and it obtains a certificate automatically. Open `https://your-domain/`. Use a dedicated domain or subdomain, not a path such as `/radio/`.

For this installation, replace `docker compose` in subsequent commands with `docker compose -f docker-compose.yaml -f compose.https.yaml`, keeping the remaining arguments. Check signing in, uploading and several minutes of listening through your domain.

## Stopping and starting

```sh
docker compose stop
docker compose start
```

The first command stops the radio; the second starts it again. Your data is preserved. Docker must start with the operating system for the radio to return after a computer restart. If you stopped the radio manually, start it again with `start`.

Music and its details are stored separately from the application in Docker storage named `music` and `data`. **Do not run `docker compose down -v`: it deletes the library.** Keep the project folder name unchanged after installation: a new name may cause Docker to use different storage, making the library appear empty.

## Saving a copy of your data

The broadcast will stop while copying. Save the music and its details together.

1. Create a new folder named `backups/before-update` inside the project folder. Use a different name for each later copy and change the command to match.
2. Run these commands in order:

```sh
docker compose stop radio
docker compose run --rm --no-deps --user 0:0 --cap-add DAC_OVERRIDE --volume "./backups/before-update:/backup" --entrypoint tar radio -czf /backup/library.tar.gz -C /app data music
docker compose start radio
```

The second command must finish without errors. If it fails, start the radio again with the last command, fix the error and repeat the copy. An incomplete archive is not a usable backup.

3. Copy `.env`, the settings file you use and, if you have a domain, `deploy/Caddyfile` into `backups/before-update`. Also save the source files of the current project version.
4. Save the built application so you can return to this version:

```sh
docker image save -o backups/before-update/image.tar radiopump:local
```

Copy the entire backup folder to another disk. It contains your password, so restrict access to it.

## Restoring your copy

These commands restore a Docker installation. They do not support moving a library previously run directly on Windows.

1. Put the saved source files, `.env` and settings files in a separate folder, keeping their original locations. Place the archive and saved application in `backups/before-update`.
2. Set an available port in `.env`, for example `RADIOPUMP_PORT=18080`. Open a terminal in this new folder.
3. Choose a new installation name that has never been used. This example uses `radiopump-restored`. Do not restore over an existing library.
4. Run these commands in order, continuing only after each one succeeds:

```sh
docker image load -i backups/before-update/image.tar
docker compose -p radiopump-restored run --rm --no-deps --user 0:0 --cap-add DAC_OVERRIDE --cap-add CHOWN --cap-add FOWNER --volume "./backups/before-update:/backup:ro" --entrypoint tar radio -xzf /backup/library.tar.gz -C /app
docker compose -p radiopump-restored up -d --no-build --wait
```

For a check on the same computer, use only the main `docker-compose.yaml`, without the HTTPS file: the original installation still uses the domain's ports. Open [http://localhost:18080/](http://localhost:18080/) and check signing in, tracks, tags, covers and sound. From now on, add `-p radiopump-restored` to every command for this installation. Playback history starts fresh after launch.

## Updating

1. Save a copy as described above and check that you can restore it.
2. Download the new project version. Replace the source files in the original folder, keeping your `.env`, `config.production.yaml` and `deploy/Caddyfile`.
3. Run:

```sh
docker compose build --pull
docker compose up -d --wait
```

4. Check signing in, the track list, uploading and sound.

If the new version fails, restore the previous application together with its matching data copy as a separate installation using the instructions above. Do not run an old version on data already changed by the new one. After checking it, stop the new version and set the previous installation to use the required port or domain.

## Troubleshooting

Show the current state and recent messages:

```sh
docker compose ps
docker compose logs --tail=100 radio
```

| Problem | What to do |
| --- | --- |
| Startup fails with a password or key error | Check both filled-in values in `.env`. |
| Port already in use | Change `RADIOPUMP_PORT` in `.env` and apply the settings. |
| Station is empty | Upload a track. For themed stations, assign a matching tag. |
| Tracks are skipped or there is no sound | Open «Состояние эфира» (Broadcast status) → «Обновить диагностику» (Refresh diagnostics) in the admin page. Damaged files are temporarily skipped; replace the affected track. |
| Upload fails | Check the format, size, free disk space and network speed. Transfers have a 60-second limit. |
| Sign-in temporarily blocked | After frequent attempts, wait a minute and try again. |
| Library disappeared after moving the folder | Restore the previous folder name and use the original Docker installation. |
| Library is busy | Stop the second radio instance using the same data. |
| Domain does not open | Check the domain address, ports 80/443 and messages from `docker compose -f docker-compose.yaml -f compose.https.yaml logs --tail=100 proxy`. |

Smooth track transitions, volume balancing and standby music for empty stations are not available yet. Full-day operation and hundreds of listeners have not been tested.
