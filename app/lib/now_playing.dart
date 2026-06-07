import 'dart:ui';

import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/cupertino.dart';
import 'package:just_audio/just_audio.dart';

import 'audio.dart';
import 'theme.dart';

/// NowPlayingScreen: immersive full-screen player with a blurred album-art
/// backdrop, properly proportioned artwork, scrubber, and transport.
class NowPlayingScreen extends StatelessWidget {
  const NowPlayingScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final player = AudioController.instance.player;
    final mq = MediaQuery.of(context);
    final artSize = (mq.size.width - 48).clamp(0.0, mq.size.height * 0.46);

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
                  filter: ImageFilter.blur(sigmaX: 45, sigmaY: 45),
                  child:
                      Container(color: kBg.withOpacity(art != null ? 0.45 : 1)),
                ),
              ),
              const Positioned.fill(
                child: DecoratedBox(
                  decoration: BoxDecoration(
                    gradient: LinearGradient(
                      begin: Alignment.topCenter,
                      end: Alignment.bottomCenter,
                      colors: [Color(0x33000000), Color(0xF20B0B0F)],
                    ),
                  ),
                ),
              ),
              SafeArea(
                child: Padding(
                  padding: const EdgeInsets.fromLTRB(24, 6, 24, 16),
                  child: Column(
                    children: [
                      // Top bar
                      SizedBox(
                        height: 44,
                        child: Row(
                          children: [
                            CupertinoButton(
                              padding: EdgeInsets.zero,
                              minSize: 36,
                              onPressed: () => Navigator.of(context).maybePop(),
                              child: const Icon(CupertinoIcons.chevron_down,
                                  color: kText, size: 26),
                            ),
                            const Expanded(
                              child: Text('NOW PLAYING',
                                  textAlign: TextAlign.center,
                                  style: TextStyle(
                                      color: kMuted,
                                      fontSize: 12,
                                      fontWeight: FontWeight.w700,
                                      letterSpacing: 1.2)),
                            ),
                            const SizedBox(width: 36),
                          ],
                        ),
                      ),
                      const Spacer(),
                      // Artwork
                      Container(
                        width: artSize,
                        height: artSize,
                        decoration: BoxDecoration(
                          borderRadius: BorderRadius.circular(16),
                          boxShadow: [
                            BoxShadow(
                                color: CupertinoColors.black.withOpacity(0.5),
                                blurRadius: 40,
                                offset: const Offset(0, 16)),
                          ],
                        ),
                        clipBehavior: Clip.antiAlias,
                        child: art != null
                            ? Image(
                                image:
                                    CachedNetworkImageProvider(art.toString()),
                                fit: BoxFit.cover)
                            : const DecoratedBox(
                                decoration:
                                    BoxDecoration(gradient: kAccentGradient),
                                child: Icon(CupertinoIcons.music_note,
                                    size: 96, color: CupertinoColors.white)),
                      ),
                      const Spacer(),
                      // Title + artist
                      SizedBox(
                        width: double.infinity,
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(item?.title ?? 'Nothing playing',
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                                style: const TextStyle(
                                    color: kText,
                                    fontSize: 24,
                                    fontWeight: FontWeight.bold)),
                            const SizedBox(height: 2),
                            Text(item?.artist ?? '',
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                                style: const TextStyle(
                                    color: kMuted, fontSize: 17)),
                          ],
                        ),
                      ),
                      const SizedBox(height: 22),
                      const _Scrubber(),
                      const SizedBox(height: 14),
                      _Transport(player: player),
                      const SizedBox(height: 24),
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
        minSize: 0,
        onPressed: onTap,
        child: Icon(icon,
            size: size,
            color: onTap == null ? kMuted.withOpacity(0.4) : kText),
      );

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisAlignment: MainAxisAlignment.spaceEvenly,
      children: [
        _btn(CupertinoIcons.backward_fill, 38,
            player.hasPrevious ? player.seekToPrevious : null),
        StreamBuilder<bool>(
          stream: player.playingStream,
          builder: (c, s) {
            final playing = s.data ?? false;
            return _btn(
                playing
                    ? CupertinoIcons.pause_circle_fill
                    : CupertinoIcons.play_circle_fill,
                80,
                playing ? player.pause : player.play);
          },
        ),
        _btn(CupertinoIcons.forward_fill, 38,
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
              max: max > 0 ? max : 1.0,
              activeColor: kText,
              thumbColor: kText,
              onChanged: (v) =>
                  player.seek(Duration(milliseconds: v.toInt())),
            ),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 8),
              child: Row(
                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                children: [
                  Text(_fmt(pos),
                      style: const TextStyle(color: kMuted, fontSize: 12)),
                  Text('-${_fmt(dur - pos)}',
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
  if (d.isNegative) d = Duration.zero;
  final m = d.inMinutes;
  final s = d.inSeconds % 60;
  return '$m:${s.toString().padLeft(2, '0')}';
}
