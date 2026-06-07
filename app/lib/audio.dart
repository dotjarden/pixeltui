import 'package:just_audio/just_audio.dart';
import 'package:just_audio_background/just_audio_background.dart';

import 'api.dart';
import 'models.dart';

/// AudioController owns the single app-wide player. just_audio_background gives
/// us background playback + lock-screen / Control-Center controls for free.
class AudioController {
  AudioController._();
  static final AudioController instance = AudioController._();

  final AudioPlayer player = AudioPlayer();

  /// playAll loads `tracks` as a queue and starts at `index`.
  Future<void> playAll(Api api, List<Track> tracks, int index) async {
    final children = tracks
        .map((t) => AudioSource.uri(
              Uri.parse(api.streamUrl(t)),
              tag: MediaItem(
                id: t.id,
                title: t.title.isEmpty ? 'Unknown' : t.title,
                artist: t.artist,
                artUri: api.artUri(t),
              ),
            ))
        .toList();
    await player.setAudioSource(
      ConcatenatingAudioSource(children: children),
      initialIndex: index,
      initialPosition: Duration.zero,
    );
    await player.play();
  }
}

/// currentItem extracts the now-playing MediaItem from a sequence state.
MediaItem? currentItem(SequenceState? seq) {
  if (seq == null) return null;
  final i = seq.currentIndex;
  if (i < 0 || i >= seq.sequence.length) return null;
  return seq.sequence[i].tag as MediaItem?;
}
