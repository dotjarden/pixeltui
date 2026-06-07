import 'dart:ui';

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

/// RootShell is the music-app frame: Home/Search/Library tabs, a persistent
/// mini-player, and a blurred bottom tab bar.
class RootShell extends StatefulWidget {
  const RootShell({super.key});
  @override
  State<RootShell> createState() => _RootShellState();
}

class _RootShellState extends State<RootShell> {
  int _tab = 0;
  Api? _api;

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
      return const CupertinoPageScaffold(
        backgroundColor: kBg,
        child: Center(child: CupertinoActivityIndicator()),
      );
    }
    return CupertinoPageScaffold(
      backgroundColor: kBg,
      child: Column(
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
          const _MiniPlayer(),
          _BottomNav(index: _tab, onTap: (i) => setState(() => _tab = i)),
        ],
      ),
    );
  }
}

class _MiniPlayer extends StatelessWidget {
  const _MiniPlayer();

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
            margin: const EdgeInsets.fromLTRB(8, 0, 8, 4),
            padding: const EdgeInsets.all(8),
            decoration: BoxDecoration(
                color: kCard, borderRadius: BorderRadius.circular(12)),
            child: Row(
              children: [
                trackArt(item.artUri, size: 42),
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
                              color: kText, fontWeight: FontWeight.w600)),
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

class _BottomNav extends StatelessWidget {
  final int index;
  final ValueChanged<int> onTap;
  const _BottomNav({required this.index, required this.onTap});

  @override
  Widget build(BuildContext context) {
    const items = [
      (CupertinoIcons.house_fill, 'Home'),
      (CupertinoIcons.search, 'Search'),
      (CupertinoIcons.music_note_list, 'Library'),
    ];
    final bottomInset = MediaQuery.of(context).padding.bottom;
    return ClipRect(
      child: BackdropFilter(
        filter: ImageFilter.blur(sigmaX: 20, sigmaY: 20),
        child: Container(
          color: kBg.withOpacity(0.75),
          padding: EdgeInsets.only(
              top: 8, bottom: bottomInset > 0 ? bottomInset : 8),
          child: Row(
            children: [
              for (var i = 0; i < items.length; i++)
                Expanded(
                  child: CupertinoButton(
                    padding: EdgeInsets.zero,
                    onPressed: () => onTap(i),
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Icon(items[i].$1,
                            size: 24, color: i == index ? kAccent : kMuted),
                        const SizedBox(height: 3),
                        Text(items[i].$2,
                            style: TextStyle(
                                fontSize: 11,
                                color: i == index ? kAccent : kMuted)),
                      ],
                    ),
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }
}
