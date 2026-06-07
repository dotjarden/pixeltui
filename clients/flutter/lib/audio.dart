import 'package:just_audio/just_audio.dart';
import 'package:just_audio_background/just_audio_background.dart';

import 'api.dart';
import 'models.dart';

/// AudioController owns the single app-wide player. just_audio_background gives
/// background playback + lock-screen / Control-Center controls.
class AudioController {
  AudioController._();
  static final AudioController instance = AudioController._();

  final AudioPlayer player = AudioPlayer();

  AudioSource _source(Api api, Track t) => AudioSource.uri(
        Uri.parse(api.streamUrl(t)),
        tag: MediaItem(
          id: t.id,
          title: t.title.isEmpty ? 'Unknown' : t.title,
          artist: t.artist,
          artUri: api.artUri(t),
        ),
      );

  /// playAll loads `tracks` as a queue and starts at `index`.
  Future<void> playAll(Api api, List<Track> tracks, int index) async {
    if (tracks.isEmpty) return;
    await player.setAudioSource(
      ConcatenatingAudioSource(
          children: [for (final t in tracks) _source(api, t)]),
      initialIndex: index,
      initialPosition: Duration.zero,
    );
    await player.play();
  }

  /// shuffleAll plays the list in random order.
  Future<void> shuffleAll(Api api, List<Track> tracks) async {
    final shuffled = [...tracks]..shuffle();
    await playAll(api, shuffled, 0);
  }
}

/// currentItem extracts the now-playing MediaItem from a sequence state.
MediaItem? currentItem(SequenceState? seq) {
  if (seq == null) return null;
  final i = seq.currentIndex;
  if (i < 0 || i >= seq.sequence.length) return null;
  return seq.sequence[i].tag as MediaItem?;
}
