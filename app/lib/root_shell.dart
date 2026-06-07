import 'package:adaptive_platform_ui/adaptive_platform_ui.dart';
import 'package:flutter/cupertino.dart';
import 'package:just_audio/just_audio.dart';

import 'api.dart';
import 'audio.dart';
import 'now_playing.dart';
import 'store.dart';
import 'tabs/home_tab.dart';
import 'tabs/library_tab.dart';
import 'tabs/search_tab.dart';
import 'theme.dart';
import 'widgets.dart';

/// RootShell is the music-app frame: a native iOS 26 liquid-glass tab bar
/// (Home/Search/Library), a liquid-glass header, and a persistent mini-player.
class RootShell extends StatefulWidget {
  const RootShell({super.key});
  @override
  State<RootShell> createState() => _RootShellState();
}

class _RootShellState extends State<RootShell> {
  int _tab = 0;
  Api? _api;
  static const _titles = ['Home', 'Search', 'Library'];

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final c = await Store.load();
    if (c != null && mounted) setState(() => _api = Api(c.url, c.token));
  }

  @override
  Widget build(BuildContext context) {
    final api = _api;
    if (api == null) {
      return AdaptiveScaffold(
        appBar: AdaptiveAppBar(title: 'pixeltui', useNativeToolbar: true),
        body: const Center(child: CupertinoActivityIndicator()),
      );
    }
    return AdaptiveScaffold(
      appBar: AdaptiveAppBar(title: _titles[_tab], useNativeToolbar: true),
      bottomNavigationBar: AdaptiveBottomNavigationBar(
        selectedIndex: _tab,
        onTap: (i) => setState(() => _tab = i),
        useNativeBottomBar: true,
        items: const [
          AdaptiveNavigationDestination(
              icon: 'house', selectedIcon: 'house.fill', label: 'Home'),
          AdaptiveNavigationDestination(
              icon: 'magnifyingglass', label: 'Search'),
          AdaptiveNavigationDestination(
              icon: 'music.note.list', label: 'Library'),
        ],
      ),
      body: Column(
        children: [
          Expanded(
            child: IndexedStack(
              index: _tab,
              children: [
                HomeTab(api: api),
                SearchTab(api: api),
                LibraryTab(api: api),
              ],
            ),
          ),
          const MiniPlayer(),
        ],
      ),
    );
  }
}

/// MiniPlayer: persistent now-playing strip; tap to open the full player.
class MiniPlayer extends StatelessWidget {
  const MiniPlayer({super.key});

  @override
  Widget build(BuildContext context) {
    final player = AudioController.instance.player;
    return StreamBuilder<SequenceState?>(
      stream: player.sequenceStateStream,
      builder: (context, snap) {
        final item = currentItem(snap.data);
        if (item == null) return const SizedBox.shrink();
        return GestureDetector(
          onTap: () => Navigator.of(context).push(CupertinoPageRoute(
              fullscreenDialog: true, builder: (_) => const NowPlayingScreen())),
          child: Container(
            margin: const EdgeInsets.fromLTRB(8, 0, 8, 6),
            padding: const EdgeInsets.all(8),
            decoration: BoxDecoration(
              color: kCard,
              borderRadius: BorderRadius.circular(12),
              border: Border.all(color: CupertinoColors.white.withOpacity(0.06)),
            ),
            child: Row(
              children: [
                trackArt(item.artUri, size: 44),
                const SizedBox(width: 10),
                Expanded(
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(item.title,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(
                              color: kText,
                              fontWeight: FontWeight.w600,
                              fontSize: 14)),
                      if ((item.artist ?? '').isNotEmpty)
                        Text(item.artist!,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style:
                                const TextStyle(color: kMuted, fontSize: 12)),
                    ],
                  ),
                ),
                StreamBuilder<bool>(
                  stream: player.playingStream,
                  builder: (c, s) {
                    final playing = s.data ?? false;
                    return CupertinoButton(
                      padding: EdgeInsets.zero,
                      onPressed: playing ? player.pause : player.play,
                      child: Icon(
                          playing
                              ? CupertinoIcons.pause_fill
                              : CupertinoIcons.play_fill,
                          color: kText,
                          size: 26),
                    );
                  },
                ),
              ],
            ),
          ),
        );
      },
    );
  }
}
