import 'package:adaptive_platform_ui/adaptive_platform_ui.dart';
import 'package:flutter/cupertino.dart';
import 'package:just_audio/just_audio.dart';

import '../audio.dart';
import '../widgets.dart';

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
          onTap: () => Navigator.of(context).push(
              CupertinoPageRoute(builder: (_) => const PlayerScreen())),
          child: Container(
            color:
                CupertinoColors.secondarySystemBackground.resolveFrom(context),
            padding: const EdgeInsets.fromLTRB(12, 8, 8, 8),
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
                          style: const TextStyle(fontWeight: FontWeight.w600)),
                      Text(item.artist ?? '',
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(
                              fontSize: 12,
                              color: CupertinoColors.systemGrey)),
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
                          size: 28),
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

/// PlayerScreen: full now-playing view with artwork, seek bar, transport.
class PlayerScreen extends StatelessWidget {
  const PlayerScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final player = AudioController.instance.player;
    return AdaptiveScaffold(
      appBar: AdaptiveAppBar(title: 'Now Playing'),
      body: SafeArea(
        child: StreamBuilder<SequenceState?>(
          stream: player.sequenceStateStream,
          builder: (context, snap) {
            final item = currentItem(snap.data);
            if (item == null) {
              return const Center(child: Text('Nothing playing'));
            }
            return Padding(
              padding: const EdgeInsets.all(24),
              child: Column(
                children: [
                  const Spacer(),
                  trackArt(item.artUri, size: 280, radius: 16),
                  const SizedBox(height: 28),
                  Text(item.title,
                      textAlign: TextAlign.center,
                      style: const TextStyle(
                          fontSize: 22, fontWeight: FontWeight.bold)),
                  const SizedBox(height: 4),
                  Text(item.artist ?? '',
                      style:
                          const TextStyle(color: CupertinoColors.systemGrey)),
                  const SizedBox(height: 20),
                  const _SeekBar(),
                  const SizedBox(height: 8),
                  Row(
                    mainAxisAlignment: MainAxisAlignment.center,
                    children: [
                      _ctrl(CupertinoIcons.backward_fill, 34,
                          player.hasPrevious ? player.seekToPrevious : null),
                      const SizedBox(width: 28),
                      StreamBuilder<bool>(
                        stream: player.playingStream,
                        builder: (c, s) {
                          final playing = s.data ?? false;
                          return _ctrl(
                              playing
                                  ? CupertinoIcons.pause_circle_fill
                                  : CupertinoIcons.play_circle_fill,
                              72,
                              playing ? player.pause : player.play);
                        },
                      ),
                      const SizedBox(width: 28),
                      _ctrl(CupertinoIcons.forward_fill, 34,
                          player.hasNext ? player.seekToNext : null),
                    ],
                  ),
                  const Spacer(),
                ],
              ),
            );
          },
        ),
      ),
    );
  }
}

Widget _ctrl(IconData icon, double size, VoidCallback? onTap) => CupertinoButton(
      padding: EdgeInsets.zero,
      onPressed: onTap,
      child: Icon(icon, size: size),
    );

class _SeekBar extends StatelessWidget {
  const _SeekBar();

  @override
  Widget build(BuildContext context) {
    final player = AudioController.instance.player;
    return StreamBuilder<Duration>(
      stream: player.positionStream,
      builder: (context, snap) {
        final pos = snap.data ?? Duration.zero;
        final dur = player.duration ?? Duration.zero;
        final max = dur.inMilliseconds.toDouble();
        final value =
            pos.inMilliseconds.toDouble().clamp(0.0, max > 0 ? max : 1.0);
        return Column(
          children: [
            AdaptiveSlider(
              value: value,
              min: 0,
              max: max > 0 ? max : 1.0,
              onChanged: (v) =>
                  player.seek(Duration(milliseconds: v.toInt())),
            ),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 6),
              child: Row(
                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                children: [
                  Text(_fmt(pos),
                      style: const TextStyle(
                          fontSize: 12, color: CupertinoColors.systemGrey)),
                  Text(_fmt(dur),
                      style: const TextStyle(
                          fontSize: 12, color: CupertinoColors.systemGrey)),
                ],
              ),
            ),
          ],
        );
      },
    );
  }
}

String _fmt(Duration d) {
  final m = d.inMinutes;
  final s = d.inSeconds % 60;
  return '$m:${s.toString().padLeft(2, '0')}';
}
