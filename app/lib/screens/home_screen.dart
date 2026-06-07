import 'package:adaptive_platform_ui/adaptive_platform_ui.dart';
import 'package:flutter/cupertino.dart';

import '../api.dart';
import '../audio.dart';
import '../models.dart';
import '../store.dart';
import '../widgets.dart';
import 'pair_screen.dart';
import 'player_screen.dart';

/// HomeScreen: native iOS chrome — segmented source picker, search, a list of
/// tracks, and a persistent mini-player.
class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key});
  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  Api? _api;
  List<String> _sources = const ['youtube'];
  int _index = 0;
  List<Track> _tracks = const [];
  bool _loading = false;
  String? _error;
  String _query = '';

  static const _labels = {
    'youtube': 'YouTube',
    'subsonic': 'Subsonic',
    'local': 'Local',
    'liked': 'Liked',
  };

  // YouTube + whichever optional sources the server reports, then Liked.
  List<String> get _visible {
    final out = <String>['youtube'];
    if (_sources.contains('subsonic')) out.add('subsonic');
    if (_sources.contains('local')) out.add('local');
    out.add('liked');
    return out;
  }

  String get _source => _visible[_index.clamp(0, _visible.length - 1)];

  @override
  void initState() {
    super.initState();
    _init();
  }

  Future<void> _init() async {
    final c = await Store.load();
    if (c == null) return _logout();
    final api = Api(c.url, c.token);
    setState(() => _api = api);
    try {
      final s = await api.sources();
      if (mounted) setState(() => _sources = s);
    } catch (_) {}
    _browse();
  }

  Future<void> _run(Future<List<Track>> Function() f) async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final t = await f();
      if (mounted) setState(() { _tracks = t; _loading = false; });
    } catch (e) {
      if (mounted) setState(() { _error = '$e'; _loading = false; });
    }
  }

  void _browse() {
    final api = _api;
    if (api == null) return;
    switch (_source) {
      case 'liked':
        _run(api.liked);
        break;
      case 'local':
        _run(api.local);
        break;
      case 'subsonic':
        _run(api.subStarred);
        break;
      default:
        setState(() => _tracks = const []); // youtube needs a search
    }
  }

  void _search() {
    final api = _api;
    if (api == null || _query.trim().isEmpty) return;
    final src =
        (_source == 'subsonic' || _source == 'local') ? _source : 'youtube';
    _run(() => api.search(src, _query.trim()));
  }

  Future<void> _logout() async {
    await Store.clear();
    if (!mounted) return;
    Navigator.of(context)
        .pushReplacement(CupertinoPageRoute(builder: (_) => const PairScreen()));
  }

  @override
  Widget build(BuildContext context) {
    return AdaptiveScaffold(
      appBar: AdaptiveAppBar(title: 'pixeltui', useNativeToolbar: true),
      body: SafeArea(
        child: Column(
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(12, 8, 4, 8),
              child: Row(
                children: [
                  Expanded(
                    child: AdaptiveTextField(
                      placeholder: 'Search ${_labels[_source] ?? ''}…',
                      prefixIcon: const Icon(CupertinoIcons.search, size: 18),
                      onChanged: (v) => _query = v,
                    ),
                  ),
                  CupertinoButton(
                    padding: const EdgeInsets.symmetric(horizontal: 10),
                    onPressed: _search,
                    child: const Icon(CupertinoIcons.arrow_right_circle_fill),
                  ),
                  CupertinoButton(
                    padding: const EdgeInsets.only(right: 6),
                    onPressed: _logout,
                    child: const Icon(CupertinoIcons.square_arrow_right, size: 22),
                  ),
                ],
              ),
            ),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 12),
              child: AdaptiveSegmentedControl(
                labels: [for (final s in _visible) _labels[s] ?? s],
                selectedIndex: _index.clamp(0, _visible.length - 1),
                onValueChanged: (i) {
                  setState(() => _index = i);
                  _browse();
                },
              ),
            ),
            if (_loading)
              const Padding(
                  padding: EdgeInsets.all(16),
                  child: CupertinoActivityIndicator()),
            if (_error != null)
              Padding(
                padding: const EdgeInsets.all(16),
                child: Text(_error!,
                    style: const TextStyle(color: CupertinoColors.systemRed)),
              ),
            Expanded(
              child: (_tracks.isEmpty && !_loading)
                  ? Center(
                      child: Text(
                        _source == 'youtube'
                            ? 'Search YouTube above'
                            : 'Nothing here yet',
                        style:
                            const TextStyle(color: CupertinoColors.systemGrey),
                      ),
                    )
                  : ListView.builder(
                      itemCount: _tracks.length,
                      itemBuilder: (context, i) {
                        final t = _tracks[i];
                        return AdaptiveListTile(
                          leading: trackArt(_api?.artUri(t)),
                          title: Text(t.title,
                              maxLines: 1, overflow: TextOverflow.ellipsis),
                          subtitle: Text(t.artist,
                              maxLines: 1, overflow: TextOverflow.ellipsis),
                          onTap: () =>
                              AudioController.instance.playAll(_api!, _tracks, i),
                        );
                      },
                    ),
            ),
            const MiniPlayer(),
          ],
        ),
      ),
    );
  }
}
