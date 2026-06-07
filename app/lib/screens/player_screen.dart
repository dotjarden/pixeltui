import 'package:flutter/material.dart';
import 'package:just_audio/just_audio.dart';

import '../audio.dart';

/// MiniPlayer is the always-visible now-playing strip at the bottom of Home.
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
        return Material(
          color: Theme.of(context).colorScheme.surfaceContainerHighest,
          child: InkWell(
            onTap: () => Navigator.of(context).push(
                MaterialPageRoute(builder: (_) => const PlayerScreen())),
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
              child: Row(
                children: [
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Text(item.title,
                            maxLines: 1, overflow: TextOverflow.ellipsis),
                        Text(item.artist ?? '',
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: const TextStyle(fontSize: 12)),
                      ],
                    ),
                  ),
                  StreamBuilder<bool>(
                    stream: player.playingStream,
                    builder: (context, s) {
                      final playing = s.data ?? false;
                      return IconButton(
                        icon: Icon(playing ? Icons.pause : Icons.play_arrow),
                        onPressed: playing ? player.pause : player.play,
                      );
                    },
                  ),
                ],
              ),
            ),
          ),
        );
      },
    );
  }
}

/// PlayerScreen is the full now-playing view with artwork + transport + seek.
class PlayerScreen extends StatelessWidget {
  const PlayerScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final player = AudioController.instance.player;
    return Scaffold(
      appBar: AppBar(title: const Text('Now Playing')),
      body: StreamBuilder<SequenceState?>(
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
                AspectRatio(
                  aspectRatio: 1,
                  child: item.artUri != null
                      ? Image.network(item.artUri.toString(),
                          fit: BoxFit.cover,
                          errorBuilder: (_, __, ___) =>
                              const Icon(Icons.music_note, size: 120))
                      : const Icon(Icons.music_note, size: 120),
                ),
                const SizedBox(height: 24),
                Text(item.title,
                    style: Theme.of(context).textTheme.titleLarge,
                    textAlign: TextAlign.center),
                Text(item.artist ?? '',
                    style: Theme.of(context).textTheme.bodyMedium),
                const SizedBox(height: 16),
                _SeekBar(player: player),
                Row(
                  mainAxisAlignment: MainAxisAlignment.center,
                  children: [
                    IconButton(
                      iconSize: 40,
                      icon: const Icon(Icons.skip_previous),
                      onPressed:
                          player.hasPrevious ? player.seekToPrevious : null,
                    ),
                    StreamBuilder<bool>(
                      stream: player.playingStream,
                      builder: (context, s) {
                        final playing = s.data ?? false;
                        return IconButton(
                          iconSize: 64,
                          icon: Icon(playing
                              ? Icons.pause_circle
                              : Icons.play_circle),
                          onPressed: playing ? player.pause : player.play,
                        );
                      },
                    ),
                    IconButton(
                      iconSize: 40,
                      icon: const Icon(Icons.skip_next),
                      onPressed: player.hasNext ? player.seekToNext : null,
                    ),
                  ],
                ),
                const Spacer(),
              ],
            ),
          );
        },
      ),
    );
  }
}

class _SeekBar extends StatelessWidget {
  final AudioPlayer player;
  const _SeekBar({required this.player});

  @override
  Widget build(BuildContext context) {
    return StreamBuilder<Duration>(
      stream: player.positionStream,
      builder: (context, snap) {
        final pos = snap.data ?? Duration.zero;
        final dur = player.duration ?? Duration.zero;
        final max = dur.inMilliseconds.toDouble();
        final value = pos.inMilliseconds.toDouble().clamp(0.0, max > 0 ? max : 1.0);
        return Slider(
          value: value,
          max: max > 0 ? max : 1.0,
          onChanged: (v) => player.seek(Duration(milliseconds: v.toInt())),
        );
      },
    );
  }
}
