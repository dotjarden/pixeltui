import 'dart:ui';

import 'package:adaptive_platform_ui/adaptive_platform_ui.dart';
import 'package:flutter/cupertino.dart';
import 'package:just_audio/just_audio.dart';

import 'api.dart';
import 'audio.dart';
import 'models.dart';
import 'now_playing.dart';
import 'store.dart';
import 'tabs/home_tab.dart';
import 'tabs/library_tab.dart';
import 'tabs/search_tab.dart';
import 'theme.dart';
import 'track_list.dart';
import 'widgets.dart';

/// An in-tab detail (a track list) kept in an in-app stack so the native tab
/// bar + mini-player stay visible on every page.
class Detail {
  final String title;
  final Future<List<Track>> Function() load;
  const Detail(this.title, this.load);
}

/// RootShell: native iOS 26 chrome (AdaptiveAppBar + AdaptiveBottomNavigationBar,
/// liquid glass) with a floating glass mini-player overlaid *above* the tab bar
/// (Apple-Music bottom-accessory style). Per-tab in-app navigation keeps the
/// chrome on every page.
class RootShell extends StatefulWidget {
  const RootShell({super.key});
  @override
  State<RootShell> createState() => _RootShellState();
}

class _RootShellState extends State<RootShell> {
  int _tab = 0;
  Api? _api;
  bool _npOpen = false;
  static const _titles = ['Home', 'Search', 'Library'];
  final List<List<Detail>> _stacks = [[], [], []];

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final c = await Store.load();
    if (c != null && mounted) setState(() => _api = Api(c.url, c.token));
  }

  void _open(String title, Future<List<Track>> Function() load) =>
      setState(() => _stacks[_tab].add(Detail(title, load)));

  void _pop() {
    if (_stacks[_tab].isNotEmpty) setState(() => _stacks[_tab].removeLast());
  }

  void _selectTab(int i) => setState(() {
        if (i == _tab && _stacks[i].isNotEmpty) {
          _stacks[i].clear();
        } else {
          _tab = i;
        }
      });

  bool get _inDetail => _stacks[_tab].isNotEmpty;
  String get _title => _inDetail ? _stacks[_tab].last.title : _titles[_tab];

  Future<void> _openNowPlaying() async {
    setState(() => _npOpen = true);
    await Navigator.of(context).push(CupertinoPageRoute(
        fullscreenDialog: true, builder: (_) => const NowPlayingScreen()));
    if (mounted) setState(() => _npOpen = false);
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
    final mq = MediaQuery.of(context);
    // Float the mini-player above the native tab bar.
    final miniBottom = mq.padding.bottom + kTabBarHeight + 6;
    // Content scrolls under the translucent header and clears the floating
    // mini-player + tab bar at the bottom.
    final pad = EdgeInsets.only(
      top: mq.padding.top + kHeaderHeight,
      bottom: miniBottom + kMiniHeight + 6,
    );

    return AdaptiveScaffold(
      extendBodyBehindAppBar: true,
      tabBarHidden: _npOpen,
      appBar: AdaptiveAppBar(
        title: _title,
        useNativeToolbar: true,
        leading: _inDetail
            ? GestureDetector(
                onTap: _pop,
                child: const Icon(CupertinoIcons.back, color: kText))
            : null,
      ),
      bottomNavigationBar: AdaptiveBottomNavigationBar(
        selectedIndex: _tab,
        onTap: _selectTab,
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
      body: Stack(
        children: [
          Positioned.fill(
            child: IndexedStack(
              index: _tab,
              children: [
                _tabContent(0, api, pad),
                _tabContent(1, api, pad),
                _tabContent(2, api, pad),
              ],
            ),
          ),
          Positioned(
            left: 10,
            right: 10,
            bottom: miniBottom,
            child: MiniPlayer(onTap: _openNowPlaying),
          ),
        ],
      ),
    );
  }

  Widget _tabContent(int t, Api api, EdgeInsets pad) {
    if (_stacks[t].isNotEmpty) {
      final d = _stacks[t].last;
      return TrackListBody(
          api: api, title: d.title, load: d.load, padding: pad, key: ValueKey(d));
    }
    switch (t) {
      case 0:
        return HomeTab(api: api, onOpen: _open, padding: pad);
      case 1:
        return SearchTab(api: api, padding: pad);
      default:
        return LibraryTab(api: api, onOpen: _open, padding: pad);
    }
  }
}

/// MiniPlayer: a floating Liquid-Glass capsule (frosted blur + hairline border)
/// like the iOS 26 Apple Music bottom accessory. Tap to open the full player.
class MiniPlayer extends StatelessWidget {
  final VoidCallback onTap;
  const MiniPlayer({super.key, required this.onTap});

  @override
  Widget build(BuildContext context) {
    final player = AudioController.instance.player;
    return StreamBuilder<SequenceState?>(
      stream: player.sequenceStateStream,
      builder: (context, snap) {
        final item = currentItem(snap.data);
        if (item == null) return const SizedBox.shrink();
        return GestureDetector(
          onTap: onTap,
          child: Container(
            decoration: BoxDecoration(
              borderRadius: BorderRadius.circular(20),
              boxShadow: [
                BoxShadow(
                    color: CupertinoColors.black.withOpacity(0.4),
                    blurRadius: 24,
                    offset: const Offset(0, 8)),
              ],
            ),
            child: ClipRRect(
              borderRadius: BorderRadius.circular(20),
              child: BackdropFilter(
                filter: ImageFilter.blur(sigmaX: 28, sigmaY: 28),
                child: Container(
                  height: kMiniHeight,
                  padding: const EdgeInsets.symmetric(horizontal: 10),
                  decoration: BoxDecoration(
                    // Frosted glass tint over the blurred backdrop.
                    color: CupertinoColors.white.withOpacity(0.10),
                    borderRadius: BorderRadius.circular(20),
                    border: Border.all(
                        color: CupertinoColors.white.withOpacity(0.16),
                        width: 0.8),
                  ),
                  child: Row(
                    children: [
                      trackArt(item.artUri, size: 44, radius: 10),
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
                                    fontSize: 15)),
                            if ((item.artist ?? '').isNotEmpty)
                              Text(item.artist!,
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                  style: const TextStyle(
                                      color: kMuted, fontSize: 12)),
                          ],
                        ),
                      ),
                      StreamBuilder<bool>(
                        stream: player.playingStream,
                        builder: (c, s) {
                          final playing = s.data ?? false;
                          return CupertinoButton(
                            padding: const EdgeInsets.symmetric(horizontal: 6),
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
                      CupertinoButton(
                        padding: const EdgeInsets.only(left: 2, right: 4),
                        onPressed:
                            player.hasNext ? player.seekToNext : null,
                        child: Icon(CupertinoIcons.forward_fill,
                            color: player.hasNext
                                ? kText
                                : kMuted.withOpacity(0.4),
                            size: 22),
                      ),
                    ],
                  ),
                ),
              ),
            ),
          ),
        );
      },
    );
  }
}
