/// Track mirrors the server's track DTO (see server/server.go).
class Track {
  final String id; // opaque stream id (lo:/su:/yt:)
  final String title;
  final String artist;
  final String source;
  final int duration;
  final String? art;

  Track({
    required this.id,
    required this.title,
    required this.artist,
    required this.source,
    required this.duration,
    this.art,
  });

  factory Track.fromJson(Map<String, dynamic> j) => Track(
        id: j['id'] as String? ?? '',
        title: j['track'] as String? ?? '',
        artist: j['artist'] as String? ?? '',
        source: j['source'] as String? ?? '',
        duration: (j['duration'] as num?)?.toInt() ?? 0,
        art: j['art'] as String?,
      );
}
