# Yozora

_Made with love by [Maria](https://github.com/mariyaholic)._

Yozora is my Windows music detector. It publishes what you are listening to as Discord Rich Presence. I wanted a presence app that asks for almost nothing, so I cut every little thing that causes RAM and CPU bloat. On a good runtime it goes as low as 10MB of RAM with barely any CPU seconds. I hope it sits quietly on your machine and lets your music show up on its own.

## Install

Download [Yozora-Setup.exe](https://github.com/mariyaholic/yozora/releases/latest/download/Yozora-Setup.exe) from the [latest release](https://github.com/mariyaholic/yozora/releases/latest) and open it. It installs for your user only, adds a Start menu entry, and asks no admin questions. That's the whole install, I promise.

Yozora is not code-signed yet, so Windows SmartScreen may say "Windows protected your PC": choose **More info**, then **Run anyway**. If you like checking things yourself, the checksums are in `SHA256SUMS.txt` on the release page. A portable `Yozora.exe` is there too.

## Everyday use

- Yozora sits in the notification area. **Open control panel** from the tray shows the panel; **Quit Yozora** stops everything.
- Tick **Start Yozora when I sign in to Windows** in the panel if you want it to run on its own.
- **Pause sharing for 1 hour** or **Pause until I resume** in the tray menu keeps a track off your profile. **Resume sharing** puts it back.
- Yozora checks for a new release about once a day and asks before it installs anything. When one is out, an **Update** row appears in the tray menu.
- To remove it: **Settings > Apps > Installed apps > Yozora > Uninstall**. It asks before it touches your settings.

The setup checklist in the panel walks you through the first start: Discord found, media detected, card visible. If Discord accepts the card but nothing shows, check **Share my activity** in Discord's own Activity Privacy settings.

Coming from v0.1.1? The installer replaces its separate `yozora-daemon-...exe` and `yozora-panel-...exe` files, and the old daemon file opens a console window when double-clicked, so delete it. Sorry for the little bit of housekeeping.

## Everything else

The fine detail lives in [Yozora's guide](https://mariyaholic.github.io/docs/yozora/) on my site:

- [Connect the Spotify Web API](https://mariyaholic.github.io/docs/yozora/#spotify) for exact artwork and track links
- [Shape the card](https://mariyaholic.github.io/docs/yozora/#card) — presets, text templates, hiding tracks, backups
- [Themes, motion and language](https://mariyaholic.github.io/docs/yozora/#look)
- [Build it from source](https://mariyaholic.github.io/docs/yozora/#build)
- [The settings file, speed and logs](https://mariyaholic.github.io/docs/yozora/#settings)

## Notes

Yozora is not affiliated with or endorsed by Discord, Spotify, or Apple.

MIT, see [LICENSE](LICENSE).

Thank you for stopping by. ♡
