import 'package:flutter/cupertino.dart';

import '../api.dart';
import '../models.dart';
import '../pair_screen.dart';
import '../store.dart';
import '../theme.dart';
import '../track_list.dart';
import '../widgets.dart';

/// LibraryTab: every source + your playlists, plus unpair.
class LibraryTab extends StatefulWidget {
  final Api api;
  const LibraryTab({super.key, required this.api});
  @override
  State<LibraryTab> createState() => _LibraryTabState();
}

class _LibraryTabState extends State<LibraryTab> {
  List<String> _sources = const ['youtube'];
  List<String> _playlists = const [];

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final s = await widget.api.sources();
      if (mounted) setState(() => _sources = s);
    } catch (_) {}
    try {
      final p = await widget.api.playlists();
      if (mounted) setState(() => _playlists = p);
    } catch (_) {}
  }

  void _open(String title, Future<List<Track>> Function() load) {
    Navigator.of(context).push(CupertinoPageRoute(
        builder: (_) =>
            TrackListScreen(title: title, api: widget.api, load: load)));
  }

  Future<void> _logout() async {
    await Store.clear();
    if (!mounted) return;
    Navigator.of(context, rootNavigator: true).pushAndRemoveUntil(
        CupertinoPageRoute(builder: (_) => const PairScreen()), (r) => false);
  }

  @override
  Widget build(BuildContext context) {
    final entries = <Widget>[
      _row(CupertinoIcons.heart_fill, 'Liked Songs',
          () => _open('Liked Songs', widget.api.liked)),
      if (_sources.contains('local'))
        _row(CupertinoIcons.folder_fill, 'Local Files',
            () => _open('Local', widget.api.local)),
      if (_sources.contains('subsonic'))
        _row(CupertinoIcons.star_fill, 'Subsonic Starred',
            () => _open('Subsonic', widget.api.subStarred)),
    ];
    return ListView(
      padding: const EdgeInsets.only(top: 8),
      children: [
        ...entries,
        if (_playlists.isNotEmpty) sectionTitle('Playlists'),
        for (final name in _playlists)
          _row(CupertinoIcons.music_note_list, name,
              () => _open(name, () => widget.api.playlist(name))),
        const SizedBox(height: 24),
        Center(
          child: CupertinoButton(
            onPressed: _logout,
            child: const Text('Unpair this device',
                style: TextStyle(color: CupertinoColors.systemRed)),
          ),
        ),
        const SizedBox(height: 16),
      ],
    );
  }

  Widget _row(IconData icon, String title, VoidCallback onTap) =>
      CupertinoButton(
        padding: EdgeInsets.zero,
        onPressed: onTap,
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
          child: Row(
            children: [
              Container(
                width: 46,
                height: 46,
                decoration: BoxDecoration(
                    color: kCard2, borderRadius: BorderRadius.circular(8)),
                child: Icon(icon, color: kAccent),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Text(title,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(
                        color: kText,
                        fontSize: 16,
                        fontWeight: FontWeight.w500)),
              ),
              const Icon(CupertinoIcons.chevron_right, color: kMuted, size: 16),
            ],
          ),
        ),
      );
}
