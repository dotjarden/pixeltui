import 'package:flutter/cupertino.dart';
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
    return CupertinoApp(
      title: 'pixeltui',
      debugShowCheckedModeBanner: false,
      theme: const CupertinoThemeData(
        brightness: Brightness.dark,
        primaryColor: kAccent,
        scaffoldBackgroundColor: kBg,
        barBackgroundColor: kBg,
      ),
      home: paired ? const RootShell() : const PairScreen(),
    );
  }
}
