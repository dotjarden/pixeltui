import 'package:flutter/cupertino.dart';

import 'api.dart';
import 'audio.dart';
import 'models.dart';
import 'theme.dart';
import 'widgets.dart';

/// TrackListBody loads + shows a list of tracks (Liked, a playlist, Local,
/// Subsonic…) with a gradient header (Play / Shuffle). It is content-only —
/// RootShell provides the liquid-glass header + tab bar + mini-player around it.
class TrackListBody extends StatefulWidget {
  final String title;
  final Api api;
  final Future<List<Track>> Function() load;
  final EdgeInsets padding;

  const TrackListBody({
    super.key,
    required this.title,
    required this.api,
    required this.load,
    this.padding = EdgeInsets.zero,
  });

  @override
  State<TrackListBody> createState() => _TrackListBodyState();
}

class _TrackListBodyState extends State<TrackListBody> {
  List<Track> _tracks = const [];
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final t = await widget.load();
      if (mounted) setState(() {
        _tracks = t;
        _loading = false;
      });
    } catch (e) {
      if (mounted) setState(() {
        _error = '$e';
        _loading = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_loading) {
      return Padding(
        padding: EdgeInsets.only(top: widget.padding.top),
        child: const Center(child: CupertinoActivityIndicator()),
      );
    }
    if (_error != null) {
      return Padding(
        padding: widget.padding,
        child: Center(
          child: Text(_error!,
              textAlign: TextAlign.center,
              style: const TextStyle(color: CupertinoColors.systemRed)),
        ),
      );
    }
    return ListView.builder(
      padding: widget.padding,
      itemCount: _tracks.length + 1,
      itemBuilder: (context, i) {
        if (i == 0) return _header();
        final t = _tracks[i - 1];
        return TrackTile(
          track: t,
          api: widget.api,
          onTap: () => playList(widget.api, _tracks, i - 1),
        );
      },
    );
  }

  Widget _header() {
    return Container(
      margin: const EdgeInsets.fromLTRB(16, 8, 16, 4),
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        gradient: kAccentGradient,
        borderRadius: BorderRadius.circular(16),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(widget.title,
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: const TextStyle(
                  color: CupertinoColors.white,
                  fontSize: 26,
                  fontWeight: FontWeight.bold)),
          Text(
            '${_tracks.length} song${_tracks.length == 1 ? '' : 's'}',
            style: TextStyle(color: CupertinoColors.white.withOpacity(0.85)),
          ),
          const SizedBox(height: 14),
          Row(
            children: [
              _pill(CupertinoIcons.play_fill, 'Play',
                  () => playList(widget.api, _tracks, 0)),
              const SizedBox(width: 12),
              _pill(CupertinoIcons.shuffle, 'Shuffle',
                  () => AudioController.instance.shuffleAll(widget.api, _tracks)),
            ],
          ),
        ],
      ),
    );
  }

  Widget _pill(IconData icon, String label, VoidCallback onTap) {
    return CupertinoButton(
      padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 8),
      color: CupertinoColors.white.withOpacity(0.18),
      borderRadius: BorderRadius.circular(20),
      minSize: 0,
      onPressed: _tracks.isEmpty ? null : onTap,
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 16, color: CupertinoColors.white),
          const SizedBox(width: 6),
          Text(label,
              style: const TextStyle(
                  color: CupertinoColors.white, fontWeight: FontWeight.w600)),
        ],
      ),
    );
  }
}
