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
flutter run
```

Scan the QR in the app → browse a source → tap a track. Audio plays on the phone
and keeps going in the background with lock-screen controls.

YouTube playback requires `yt-dlp` + `ffmpeg` on the **server** (`pixeltui
doctor --fix` installs them); Subsonic and local need nothing extra.

## Distribution

- **Personal:** `flutter build ipa` → TestFlight, or run via Xcode on your own
  device.
- **Public (App Store):** position it as a client for your self-hosted server
  (Subsonic/local). Restrict `NSAppTransportSecurity` and prefer a TLS tunnel.

## Structure

```
lib/
  main.dart            app entry, background-audio init, routing
  api.dart             REST client for `pixeltui serve`
  models.dart          Track DTO (mirrors the server)
  store.dart           secure storage of {url, token}
  audio.dart           single AudioPlayer (just_audio + just_audio_background)
  screens/pair_screen.dart     QR / manual pairing
  screens/home_screen.dart     source picker, search, browse, list
  screens/player_screen.dart   mini-player + full Now-Playing
```
