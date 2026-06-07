import 'package:adaptive_platform_ui/adaptive_platform_ui.dart';
import 'package:flutter/cupertino.dart';

import 'api.dart';
import 'audio.dart';
import 'models.dart';
import 'theme.dart';
import 'widgets.dart';

/// TrackListScreen loads a list of tracks (Liked, a playlist, Local, Subsonic…)
/// and shows a gradient header with Play / Shuffle plus the rows.
class TrackListScreen extends StatefulWidget {
  final String title;
  final String subtitle;
  final Api api;
  final Future<List<Track>> Function() load;

  const TrackListScreen({
    super.key,
    required this.title,
    this.subtitle = '',
    required this.api,
    required this.load,
  });

  @override
  State<TrackListScreen> createState() => _TrackListScreenState();
}

class _TrackListScreenState extends State<TrackListScreen> {
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
    return AdaptiveScaffold(
      appBar: AdaptiveAppBar(title: widget.title, useNativeToolbar: true),
      body: _loading
          ? const Center(child: CupertinoActivityIndicator())
          : _error != null
              ? Center(
                  child: Padding(
                    padding: const EdgeInsets.all(24),
                    child: Text(_error!,
                        textAlign: TextAlign.center,
                        style:
                            const TextStyle(color: CupertinoColors.systemRed)),
                  ),
                )
              : CustomScrollView(
                  slivers: [
                    SliverToBoxAdapter(child: _header()),
                    SliverList(
                      delegate: SliverChildBuilderDelegate(
                        (context, i) => TrackTile(
                          track: _tracks[i],
                          api: widget.api,
                          onTap: () => playList(widget.api, _tracks, i),
                        ),
                        childCount: _tracks.length,
                      ),
                    ),
                    const SliverToBoxAdapter(child: SizedBox(height: 24)),
                  ],
                ),
    );
  }

  Widget _header() {
    return Container(
      margin: const EdgeInsets.fromLTRB(16, 12, 16, 4),
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        gradient: kAccentGradient,
        borderRadius: BorderRadius.circular(16),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(widget.title,
              style: const TextStyle(
                  color: CupertinoColors.white,
                  fontSize: 26,
                  fontWeight: FontWeight.bold)),
          Text(
            widget.subtitle.isNotEmpty
                ? widget.subtitle
                : '${_tracks.length} song${_tracks.length == 1 ? '' : 's'}',
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
