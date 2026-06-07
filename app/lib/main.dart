import 'package:flutter/material.dart';
import 'package:just_audio_background/just_audio_background.dart';

import 'screens/home_screen.dart';
import 'screens/pair_screen.dart';
import 'store.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  await JustAudioBackground.init(
    androidNotificationChannelId: 'net.dotjarden.pixeltui.audio',
    androidNotificationChannelName: 'pixeltui',
    androidNotificationOngoing: true,
  );
  final creds = await Store.load();
  runApp(PixeltuiApp(paired: creds != null));
}

class PixeltuiApp extends StatelessWidget {
  final bool paired;
  const PixeltuiApp({super.key, required this.paired});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'pixeltui',
      debugShowCheckedModeBanner: false,
      theme: ThemeData.dark(useMaterial3: true).copyWith(
        colorScheme: ColorScheme.fromSeed(
          seedColor: const Color(0xFF7D56F4), // pixeltui purple
          brightness: Brightness.dark,
        ),
      ),
      home: paired ? const HomeScreen() : const PairScreen(),
    );
  }
}
