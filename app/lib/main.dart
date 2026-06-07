import 'package:adaptive_platform_ui/adaptive_platform_ui.dart';
import 'package:flutter/cupertino.dart';
import 'package:flutter/material.dart';
import 'package:just_audio_background/just_audio_background.dart';

import 'pair_screen.dart';
import 'root_shell.dart';
import 'store.dart';
import 'theme.dart';

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
    // The app is dark-only for a consistent music-app aesthetic.
    final dark = ThemeData.dark(useMaterial3: true).copyWith(
      scaffoldBackgroundColor: kBg,
      colorScheme:
          ColorScheme.fromSeed(seedColor: kAccent, brightness: Brightness.dark),
    );
    const cupertino = CupertinoThemeData(
      brightness: Brightness.dark,
      primaryColor: kAccent,
      scaffoldBackgroundColor: kBg,
    );
    return AdaptiveApp(
      title: 'pixeltui',
      themeMode: ThemeMode.dark,
      materialDarkTheme: dark,
      materialLightTheme: dark,
      cupertinoDarkTheme: cupertino,
      cupertinoLightTheme: cupertino,
      home: paired ? const RootShell() : const PairScreen(),
    );
  }
}
