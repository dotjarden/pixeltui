import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/cupertino.dart';

import 'api.dart';
import 'audio.dart';
import 'models.dart';
import 'theme.dart';

/// trackArt renders cached cover art with a gradient music-note fallback.
Widget trackArt(Uri? uri, {double size = 48, double radius = 8}) {
  final fallback = Container(
    width: size,
    height: size,
    decoration: BoxDecoration(
      gradient: kAccentGradient,
      borderRadius: BorderRadius.circular(radius),
    ),
    child: Icon(CupertinoIcons.music_note,
        size: size * 0.42, color: CupertinoColors.white.withOpacity(0.9)),
  );
  if (uri == null) return fallback;
  return ClipRRect(
    borderRadius: BorderRadius.circular(radius),
    child: CachedNetworkImage(
      imageUrl: uri.toString(),
      width: size,
      height: size,
      fit: BoxFit.cover,
      fadeInDuration: const Duration(milliseconds: 150),
      placeholder: (_, __) => fallback,
      errorWidget: (_, __, ___) => fallback,
    ),
  );
}

/// sectionTitle is a bold left-aligned header like Apple Music's sections.
Widget sectionTitle(String text, {EdgeInsets? padding}) => Padding(
      padding: padding ?? const EdgeInsets.fromLTRB(16, 18, 16, 8),
      child: Text(text,
          style: const TextStyle(
              color: kText, fontSize: 22, fontWeight: FontWeight.bold)),
    );

/// TrackTile is a uniform now-playing-aware row used in every list.
class TrackTile extends StatelessWidget {
  final Track track;
  final Api api;
  final VoidCallback onTap;
  const TrackTile(
      {super.key, required this.track, required this.api, required this.onTap});

  @override
  Widget build(BuildContext context) {
    return CupertinoButton(
      padding: EdgeInsets.zero,
      onPressed: onTap,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
        child: Row(
          children: [
            trackArt(api.artUri(track), size: 50),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(track.title,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(
                          color: kText,
                          fontSize: 16,
                          fontWeight: FontWeight.w500)),
                  if (track.artist.isNotEmpty)
                    Text(track.artist,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(color: kMuted, fontSize: 13)),
                ],
              ),
            ),
            const Icon(CupertinoIcons.ellipsis,
                color: kMuted, size: 18),
          ],
        ),
      ),
    );
  }
}

/// playList is the shared "tap a row → play the whole list from here" action.
void playList(Api api, List<Track> tracks, int index) =>
    AudioController.instance.playAll(api, tracks, index);
