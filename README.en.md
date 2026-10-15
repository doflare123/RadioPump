# RadioPump

[Русский](README.md) · English

Your own music radio on a computer or server. Upload music, then let listeners open the website and choose a station. Each station has one continuous broadcast: new listeners join the track already playing.

- The main station plays the whole library; themed stations play tracks with selected tags.
- Track details and cover art are read from the file. You can edit the fields before uploading.
- The website shows the current track, five previous tracks and five upcoming tracks.
- An administrator manages the music. Listening does not require signing in.

## Start

1. Install Docker with Compose. In Docker Desktop, select Linux containers.
2. Download the project using **Code → Download ZIP**, extract it and open a terminal in the folder containing `docker-compose.yaml`.
3. Copy `.env.example` to `.env`. Set `RADIOPUMP_ADMIN_PASSWORD` to your own password and `RADIOPUMP_JWT_SECRET` to a random key. [How to fill these in](docs/guide.en.md#first-start).
4. Run:

```sh
docker compose up -d --build --wait
```

Open the [radio](http://localhost:8080/), then the [admin sign-in page](http://localhost:8080/admin-login.html). Use `admin` and the password from `.env`. Upload your first track, then play the station on the home page.

The first build needs internet access. Docker installs the other required programs. There is no sound until you upload music. Keep the computer and Docker running.

## Documentation

In development: a [Flutter application](apps/radiopump) with station selection,
multiple server profiles and a desktop tray mini-player.
[How to run it](docs/guide.en.md#application).

[User guide](docs/guide.en.md): uploading music, setting up stations, access from other devices and the internet, saving your data, updates and troubleshooting. The current web interface is in Russian; the guide includes the button names.

[Architecture diagram](docs/radio-architecture-corrected.drawio.png) · [Editable diagram](docs/radio-architecture-corrected.drawio)

Automatic folder import, smooth track transitions and volume balancing are not available yet. Extended operation under heavy load has not been tested.

## License

[CC BY-NC 4.0](LICENSE): non-commercial use with attribution.
