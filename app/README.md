# pixeltui — companion app

A Flutter client for [`pixeltui serve`](../README.md#companion-server-experimental).
It pairs to your server once (scan the QR), then browses, searches, and **plays
your music in the background** (lock-screen / Control-Center controls) from
anywhere via your tunnel.

> This is the source (`lib/` + `pubspec.yaml`). Generate the platform projects
> and dependencies before first run.

## Build

```sh
cd app
flutter create --org net.dotjarden --platforms=ios,android .   # one-time: makes ios/ android/
flutter pub get
```

`flutter create` adds the `ios/` and `android/` runners without touching `lib/`.

## iOS setup (required for background audio + QR + LAN http)

Edit `ios/Runner/Info.plist` and add:

```xml
<key>UIBackgroundModes</key>
<array><string>audio</string></array>

<key>NSCameraUsageDescription</key>
<string>Scan the pairing QR from pixeltui.</string>

<!-- Allow plain http to a LAN server / non-TLS tunnel. Tighten for App Store. -->
<key>NSAppTransportSecurity</key>
<dict><key>NSAllowsArbitraryLoads</key><true/></dict>
```

Minimum iOS 13 (`ios/Podfile`: `platform :ios, '13.0'`).

## Run

```sh
# 1. on your computer:
pixeltui serve                      # prints a pairing QR (+ code)
#    (for off-network use, front it with a tunnel and: pixeltui serve --url https://…)

# 2. on your iPhone (connected, developer mode):
flutter run -d <device-udid>        # see `flutter devices` for the id
```

Scan the QR in the app → browse a source → tap a track. Audio plays on the phone
and keeps going in the background with lock-screen controls.

YouTube playback requires `yt-dlp` + `ffmpeg` on the **server** (`pixeltui
doctor --fix` installs them); Subsonic and local need nothing extra.

## Troubleshooting

- **`pod install` SSL error** (`certificate verify failed`): point Ruby at the
  system CA bundle, then retry:
  ```sh
  export SSL_CERT_FILE=/etc/ssl/cert.pem
  cd ios && pod install --repo-update && cd ..
  ```
- **iPhone not in the `flutter run` picker** (wireless is flaky on iOS 26):
  target it directly — `flutter run -d <udid> --device-timeout 60` — or use USB.
- **White screen + "Dart VM Service was not discovered"**: that's the wireless
  *debug* attach failing, not the app. Use USB, or run release: `flutter run
  --release -d <udid>`.
- **"No MaterialLocalizations found"** (rare): add `flutter_localizations` (sdk)
  and pass the Global*Localizations delegates to `AdaptiveApp`.

## Distribution

- **Personal:** `flutter build ipa` → TestFlight, or run via Xcode on your own
  device.
- **Public (App Store):** position it as a client for your self-hosted server
  (Subsonic/local). Restrict `NSAppTransportSecurity` and prefer a TLS tunnel.

## Structure

```
lib/
  main.dart          app entry, background-audio init, dark theme, routing
  theme.dart         dark palette + accent gradient
  api.dart           REST client for `pixeltui serve`
  models.dart        Track DTO (mirrors the server)
  store.dart         secure storage of {url, token}
  audio.dart         single AudioPlayer (just_audio + just_audio_background)
  widgets.dart       cached cover art, TrackTile, section headers
  pair_screen.dart   QR / manual pairing
  root_shell.dart    tab bar + persistent mini-player frame
  now_playing.dart   full-screen player (blurred art backdrop, scrubber)
  track_list.dart    reusable list screen (gradient header, Play/Shuffle)
  tabs/home_tab.dart       quick-access cards + playlists
  tabs/search_tab.dart     search field + source selector + results
  tabs/library_tab.dart    all sources + playlists + unpair
```

The UI is built with Cupertino widgets for a native iOS feel; `AdaptiveApp`
(from `adaptive_platform_ui`) is the root (renders Material on Android). If
`AdaptiveApp` ever errors, swap it for `CupertinoApp` in `main.dart` — the
screens are Cupertino either way.
