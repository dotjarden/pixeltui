import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/cupertino.dart';

/// trackArt renders cached cover art (fast, no re-fetch) with a music-note
/// fallback while loading or when there's no art.
Widget trackArt(Uri? uri, {double size = 46, double radius = 8}) {
  final fallback = Container(
    width: size,
    height: size,
    decoration: BoxDecoration(
      color: CupertinoColors.systemGrey5,
      borderRadius: BorderRadius.circular(radius),
    ),
    child: Icon(CupertinoIcons.music_note,
        size: size * 0.45, color: CupertinoColors.systemGrey),
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
