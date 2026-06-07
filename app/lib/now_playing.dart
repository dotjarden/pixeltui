import 'dart:ui';

import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/cupertino.dart';
import 'package:just_audio/just_audio.dart';

import 'audio.dart';
import 'theme.dart';

/// NowPlayingScreen: full-screen player with a blurred album-art backdrop,
/// scrubber, and transport — Apple-Music style. Pushed as a slide-up sheet.
class NowPlayingScreen extends StatelessWidget {
  const NowPlayingScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final player = AudioController.instance.player;
    return StreamBuilder<SequenceState?>(
      stream: player.sequenceStateStream,
      builder: (context, snap) {
        final item = currentItem(snap.data);
        final art = item?.artUri;
        return CupertinoPageScaffold(
          backgroundColor: kBg,
          child: Stack(
            fit: StackFit.expand,
            children: [
              if (art != null)
                Image(
                    image: CachedNetworkImageProvider(art.toString()),
                    fit: BoxFit.cover),
              Positioned.fill(
                child: BackdropFilter(
                  filter: ImageFilter.blur(sigmaX: 60, sigmaY: 60),
                  child: Container(
                      color: kBg.withOpacity(art != null ? 0.5 : 1)),
                ),
              ),
              const Positioned.fill(
                child: DecoratedBox(
                  decoration: BoxDecoration(
                    gradient: LinearGradient(
                      begin: Alignment.topCenter,
                      end: Alignment.bottomCenter,
                      colors: [Color(0x22000000), Color(0xEE0B0B0F)],
                    ),
                  ),
                ),
              ),
              SafeArea(
                child: Padding(
                  padding: const EdgeInsets.symmetric(horizontal: 28),
                  child: Column(
                    children: [
                      Padding(
                        padding: const EdgeInsets.only(top: 4, bottom: 4),
                        child: Row(
                          children: [
                            CupertinoButton(
                              padding: EdgeInsets.zero,
                              onPressed: () => Navigator.of(context).maybePop(),
                              child: const Icon(CupertinoIcons.chevron_down,
                                  color: kText),
                            ),
                            const Spacer(),
                            const Text('Now Playing',
                                style: TextStyle(
                                    color: kText,
                                    fontWeight: FontWeight.w600)),
                            const Spacer(),
                            const SizedBox(width: 44),
                          ],
                        ),
                      ),
                      const Spacer(),
                      AspectRatio(
                        aspectRatio: 1,
                        child: ClipRRect(
                          borderRadius: BorderRadius.circular(18),
                          child: art != null
                              ? Image(
                                  image: CachedNetworkImageProvider(
                                      art.toString()),
                                  fit: BoxFit.cover)
                              : Container(
                                  decoration: const BoxDecoration(
                                      gradient: kAccentGradient),
                                  child: Icon(CupertinoIcons.music_note,
                                      size: 96,
                                      color:
                                          CupertinoColors.white.withOpacity(0.9)),
                                ),
                        ),
                      ),
                      const Spacer(),
                      Align(
                        alignment: Alignment.centerLeft,
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            Text(item?.title ?? 'Nothing playing',
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                                style: const TextStyle(
                                    color: kText,
                                    fontSize: 24,
                                    fontWeight: FontWeight.bold)),
                            if ((item?.artist ?? '').isNotEmpty)
                              Text(item!.artist!,
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                  style: const TextStyle(
                                      color: kMuted, fontSize: 17)),
                          ],
                        ),
                      ),
                      const SizedBox(height: 18),
                      const _Scrubber(),
                      const SizedBox(height: 10),
                      _Transport(player: player),
                      const Spacer(),
                    ],
                  ),
                ),
              ),
            ],
          ),
        );
      },
    );
  }
}

class _Transport extends StatelessWidget {
  final AudioPlayer player;
  const _Transport({required this.player});

  Widget _btn(IconData icon, double size, VoidCallback? onTap) =>
      CupertinoButton(
        padding: EdgeInsets.zero,
        onPressed: onTap,
        child: Icon(icon, size: size, color: kText),
      );

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisAlignment: MainAxisAlignment.center,
      children: [
        _btn(CupertinoIcons.backward_fill, 36,
            player.hasPrevious ? player.seekToPrevious : null),
        const SizedBox(width: 36),
        StreamBuilder<bool>(
          stream: player.playingStream,
          builder: (c, s) {
            final playing = s.data ?? false;
            return _btn(
                playing
                    ? CupertinoIcons.pause_circle_fill
                    : CupertinoIcons.play_circle_fill,
                78,
                playing ? player.pause : player.play);
          },
        ),
        const SizedBox(width: 36),
        _btn(CupertinoIcons.forward_fill, 36,
            player.hasNext ? player.seekToNext : null),
      ],
    );
  }
}

class _Scrubber extends StatelessWidget {
  const _Scrubber();

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
            CupertinoSlider(
              value: value,
              min: 0,
              max: max > 0 ? max : 1.0,
              activeColor: kText,
              thumbColor: kText,
              onChanged: (v) =>
                  player.seek(Duration(milliseconds: v.toInt())),
            ),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 6),
              child: Row(
                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                children: [
                  Text(_fmt(pos),
                      style: const TextStyle(color: kMuted, fontSize: 12)),
                  Text(_fmt(dur),
                      style: const TextStyle(color: kMuted, fontSize: 12)),
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
