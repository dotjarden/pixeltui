import 'package:flutter/cupertino.dart';

import '../api.dart';
import '../models.dart';
import '../theme.dart';
import '../widgets.dart';

/// HomeTab: quick-access cards + your playlists (Spotify-style landing).
class HomeTab extends StatefulWidget {
  final Api api;
  final void Function(String title, Future<List<Track>> Function() load) onOpen;
  final EdgeInsets padding;
  const HomeTab(
      {super.key,
      required this.api,
      required this.onOpen,
      this.padding = EdgeInsets.zero});
  @override
  State<HomeTab> createState() => _HomeTabState();
}

class _HomeTabState extends State<HomeTab> {
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

  void _open(String title, Future<List<Track>> Function() load) =>
      widget.onOpen(title, load);

  @override
  Widget build(BuildContext context) {
    final cards = <Widget>[
      _card('Liked Songs', CupertinoIcons.heart_fill,
          () => _open('Liked Songs', widget.api.liked)),
      if (_sources.contains('local'))
        _card('Local', CupertinoIcons.folder_fill,
            () => _open('Local', widget.api.local)),
      if (_sources.contains('subsonic'))
        _card('Subsonic', CupertinoIcons.cloud_fill,
            () => _open('Subsonic', widget.api.subStarred)),
    ];
    return ListView(
      padding: widget.padding.add(const EdgeInsets.only(top: 4, bottom: 12)),
      children: [
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 16),
          child: GridView.count(
            crossAxisCount: 2,
            shrinkWrap: true,
            physics: const NeverScrollableScrollPhysics(),
            mainAxisSpacing: 12,
            crossAxisSpacing: 12,
            childAspectRatio: 2.6,
            children: cards,
          ),
        ),
        if (_playlists.isNotEmpty) sectionTitle('Your Playlists'),
        for (final name in _playlists)
          _row(name, () => _open(name, () => widget.api.playlist(name))),
      ],
    );
  }

  Widget _card(String title, IconData icon, VoidCallback onTap) {
    return CupertinoButton(
      padding: EdgeInsets.zero,
      onPressed: onTap,
      child: Container(
        decoration: BoxDecoration(
            gradient: kAccentGradient, borderRadius: BorderRadius.circular(12)),
        padding: const EdgeInsets.all(12),
        child: Row(
          children: [
            Icon(icon, color: CupertinoColors.white, size: 22),
            const SizedBox(width: 8),
            Expanded(
              child: Text(title,
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                      color: CupertinoColors.white,
                      fontWeight: FontWeight.w700)),
            ),
          ],
        ),
      ),
    );
  }

  Widget _row(String name, VoidCallback onTap) => CupertinoButton(
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
                child: const Icon(CupertinoIcons.music_note_list, color: kMuted),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Text(name,
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
