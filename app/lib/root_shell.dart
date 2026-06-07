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

/// A pushed-in-tab detail (a track list) — kept in an in-app stack so the tab
/// bar + mini-player stay visible on every page (Apple-Music behavior).
class Detail {
  final String title;
  final Future<List<Track>> Function() load;
  const Detail(this.title, this.load);
}

/// Callback tabs use to open a track list without leaving the shell.
typedef OpenDetail = void Function(
    String title, Future<List<Track>> Function() load);

/// RootShell: one AdaptiveScaffold = liquid-glass header + native iOS 26
/// liquid-glass tab bar + persistent mini-player, with per-tab in-app
/// navigation so nav + mini show on all pages.
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
          _stacks[i].clear(); // re-tap current tab → pop to its root
        } else {
          _tab = i;
        }
      });

  bool get _inDetail => _stacks[_tab].isNotEmpty;
  String get _title => _inDetail ? _stacks[_tab].last.title : _titles[_tab];

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
      body: Column(
        children: [
          Expanded(
            child: SafeArea(
              bottom: false,
              child: IndexedStack(
                index: _tab,
                children: [
                  _tabContent(0, api),
                  _tabContent(1, api),
                  _tabContent(2, api),
                ],
              ),
            ),
          ),
          const MiniPlayer(),
        ],
      ),
    );
  }

  Widget _tabContent(int t, Api api) {
    if (_stacks[t].isNotEmpty) {
      final d = _stacks[t].last;
      return TrackListBody(api: api, title: d.title, load: d.load, key: ValueKey(d));
    }
    switch (t) {
      case 0:
        return HomeTab(api: api, onOpen: _open);
      case 1:
        return SearchTab(api: api);
      default:
        return LibraryTab(api: api, onOpen: _open);
    }
  }
}

/// MiniPlayer: persistent floating now-playing capsule above the tab bar.
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
              borderRadius: BorderRadius.circular(14),
              border: Border.all(color: CupertinoColors.white.withOpacity(0.06)),
              boxShadow: [
                BoxShadow(
                    color: CupertinoColors.black.withOpacity(0.3),
                    blurRadius: 16,
                    offset: const Offset(0, 4)),
              ],
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
