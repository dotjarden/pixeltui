import 'dart:ui';

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

/// An in-tab detail (a track list) kept in an in-app stack so the tab bar +
/// mini-player stay visible on every page.
class Detail {
  final String title;
  final Future<List<Track>> Function() load;
  const Detail(this.title, this.load);
}

/// RootShell: custom Flutter "glass" chrome — a blurred header + blurred tab bar
/// + floating mini-player, with content inset so nothing renders behind them.
/// Per-tab in-app navigation keeps the chrome on every page.
class RootShell extends StatefulWidget {
  const RootShell({super.key});
  @override
  State<RootShell> createState() => _RootShellState();
}

class _RootShellState extends State<RootShell> {
  int _tab = 0;
  Api? _api;
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

  @override
  Widget build(BuildContext context) {
    final mq = MediaQuery.of(context);
    final api = _api;
    if (api == null) {
      return const CupertinoPageScaffold(
        backgroundColor: kBg,
        child: Center(child: CupertinoActivityIndicator()),
      );
    }

    // Content scrolls under the translucent header and bottom chrome.
    final contentPadding = EdgeInsets.only(
      top: mq.padding.top + kHeaderHeight,
      bottom: mq.padding.bottom + kTabBarHeight + kMiniHeight,
    );

    return CupertinoPageScaffold(
      backgroundColor: kBg,
      child: Stack(
        children: [
          Positioned.fill(
            child: IndexedStack(
              index: _tab,
              children: [
                _tabContent(0, api, contentPadding),
                _tabContent(1, api, contentPadding),
                _tabContent(2, api, contentPadding),
              ],
            ),
          ),
          // Glass header
          Positioned(
            top: 0,
            left: 0,
            right: 0,
            child: _GlassHeader(
              title: _title,
              topInset: mq.padding.top,
              showBack: _inDetail,
              onBack: _pop,
            ),
          ),
          // Mini-player + glass tab bar
          Positioned(
            left: 0,
            right: 0,
            bottom: 0,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                const MiniPlayer(),
                _GlassTabBar(
                  index: _tab,
                  onTap: _selectTab,
                  bottomInset: mq.padding.bottom,
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _tabContent(int t, Api api, EdgeInsets padding) {
    if (_stacks[t].isNotEmpty) {
      final d = _stacks[t].last;
      return TrackListBody(
          api: api, title: d.title, load: d.load, padding: padding, key: ValueKey(d));
    }
    switch (t) {
      case 0:
        return HomeTab(api: api, onOpen: _open, padding: padding);
      case 1:
        return SearchTab(api: api, padding: padding);
      default:
        return LibraryTab(api: api, onOpen: _open, padding: padding);
    }
  }
}

class _GlassHeader extends StatelessWidget {
  final String title;
  final double topInset;
  final bool showBack;
  final VoidCallback onBack;
  const _GlassHeader({
    required this.title,
    required this.topInset,
    required this.showBack,
    required this.onBack,
  });

  @override
  Widget build(BuildContext context) {
    return ClipRect(
      child: BackdropFilter(
        filter: ImageFilter.blur(sigmaX: 24, sigmaY: 24),
        child: Container(
          padding: EdgeInsets.only(top: topInset),
          decoration: BoxDecoration(
            color: kBg.withOpacity(0.7),
            border: Border(
                bottom: BorderSide(
                    color: CupertinoColors.white.withOpacity(0.06))),
          ),
          child: SizedBox(
            height: kHeaderHeight,
            child: Row(
              children: [
                if (showBack)
                  CupertinoButton(
                    padding: const EdgeInsets.symmetric(horizontal: 12),
                    onPressed: onBack,
                    child: const Icon(CupertinoIcons.back, color: kText),
                  )
                else
                  const SizedBox(width: 16),
                Expanded(
                  child: Text(title,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(
                          color: kText,
                          fontSize: 24,
                          fontWeight: FontWeight.bold)),
                ),
                const SizedBox(width: 16),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _GlassTabBar extends StatelessWidget {
  final int index;
  final ValueChanged<int> onTap;
  final double bottomInset;
  const _GlassTabBar({
    required this.index,
    required this.onTap,
    required this.bottomInset,
  });

  static const _items = [
    (CupertinoIcons.house_fill, 'Home'),
    (CupertinoIcons.search, 'Search'),
    (CupertinoIcons.music_note_list, 'Library'),
  ];

  @override
  Widget build(BuildContext context) {
    return ClipRect(
      child: BackdropFilter(
        filter: ImageFilter.blur(sigmaX: 24, sigmaY: 24),
        child: Container(
          padding: EdgeInsets.only(bottom: bottomInset),
          decoration: BoxDecoration(
            color: kBg.withOpacity(0.8),
            border: Border(
                top: BorderSide(
                    color: CupertinoColors.white.withOpacity(0.06))),
          ),
          child: SizedBox(
            height: kTabBarHeight,
            child: Row(
              children: [
                for (var i = 0; i < _items.length; i++)
                  Expanded(
                    child: CupertinoButton(
                      padding: EdgeInsets.zero,
                      onPressed: () => onTap(i),
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          Icon(_items[i].$1,
                              size: 23,
                              color: i == index ? kAccent : kMuted),
                          const SizedBox(height: 3),
                          Text(_items[i].$2,
                              style: TextStyle(
                                  fontSize: 10.5,
                                  fontWeight: FontWeight.w600,
                                  color: i == index ? kAccent : kMuted)),
                        ],
                      ),
                    ),
                  ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// MiniPlayer: floating now-playing capsule that sits above the tab bar.
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
            height: kMiniHeight - 8,
            margin: const EdgeInsets.fromLTRB(8, 0, 8, 6),
            padding: const EdgeInsets.all(8),
            decoration: BoxDecoration(
              color: kCard2,
              borderRadius: BorderRadius.circular(14),
              border:
                  Border.all(color: CupertinoColors.white.withOpacity(0.08)),
              boxShadow: [
                BoxShadow(
                    color: CupertinoColors.black.withOpacity(0.35),
                    blurRadius: 18,
                    offset: const Offset(0, 6)),
              ],
            ),
            child: Row(
              children: [
                trackArt(item.artUri, size: 40),
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
