import 'package:adaptive_platform_ui/adaptive_platform_ui.dart';
import 'package:flutter/cupertino.dart';
import 'package:flutter/material.dart';
import 'package:just_audio_background/just_audio_background.dart';

import 'screens/home_screen.dart';
import 'screens/pair_screen.dart';
import 'store.dart';

const seed = Color(0xFF7D56F4); // pixeltui purple

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  // Guard init so a platform hiccup can never leave the app on a white screen.
  try {
    await JustAudioBackground.init(
      androidNotificationChannelId: 'net.dotjarden.pixeltui.audio',
      androidNotificationChannelName: 'pixeltui',
      androidNotificationOngoing: true,
    );
  } catch (_) {}
  final creds = await Store.load();
  runApp(PixeltuiApp(paired: creds != null));
}

class PixeltuiApp extends StatelessWidget {
  final bool paired;
  const PixeltuiApp({super.key, required this.paired});

  @override
  Widget build(BuildContext context) {
    return AdaptiveApp(
      title: 'pixeltui',
      themeMode: ThemeMode.dark,
      materialDarkTheme: ThemeData.dark(useMaterial3: true).copyWith(
        colorScheme:
            ColorScheme.fromSeed(seedColor: seed, brightness: Brightness.dark),
      ),
      materialLightTheme: ThemeData.light(useMaterial3: true),
      cupertinoDarkTheme: const CupertinoThemeData(
          brightness: Brightness.dark, primaryColor: seed),
      cupertinoLightTheme: const CupertinoThemeData(
          brightness: Brightness.light, primaryColor: seed),
      home: paired ? const HomeScreen() : const PairScreen(),
    );
  }
}
