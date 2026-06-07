import 'dart:convert';

import 'package:http/http.dart' as http;

import 'models.dart';

/// Api is a thin client for `pixeltui serve`.
class Api {
  final String base; // e.g. http://host:8787 (no trailing slash)
  final String token;

  Api(this.base, this.token);

  Map<String, String> get _headers => {'Authorization': 'Bearer $token'};

  static String _trim(String u) =>
      u.endsWith('/') ? u.substring(0, u.length - 1) : u;

  static const _timeout = Duration(seconds: 12);

  /// pair exchanges a session code for a durable device token.
  static Future<String> pair(String url, String code,
      {String name = 'phone'}) async {
    final http.Response r;
    try {
      r = await http
          .post(
            Uri.parse('${_trim(url)}/pair'),
            headers: {'Content-Type': 'application/json'},
            body: jsonEncode({'code': code, 'name': name}),
          )
          .timeout(_timeout);
    } catch (e) {
      throw Exception("Can't reach the server at $url — same Wi-Fi / tunnel up?");
    }
    if (r.statusCode != 200) {
      throw Exception('Pairing failed (HTTP ${r.statusCode})');
    }
    return jsonDecode(r.body)['token'] as String;
  }

  Future<Map<String, dynamic>> _json(String path) async {
    final http.Response r;
    try {
      r = await http.get(Uri.parse('$base$path'), headers: _headers).timeout(_timeout);
    } catch (e) {
      throw Exception('Server unreachable — check the connection.');
    }
    if (r.statusCode == 401) throw Exception('Not paired — unpair and scan again.');
    if (r.statusCode != 200) throw Exception('HTTP ${r.statusCode}');
    return jsonDecode(r.body) as Map<String, dynamic>;
  }

  Future<List<Track>> _tracks(String path) async {
    final j = await _json(path);
    return (j['tracks'] as List? ?? [])
        .map((e) => Track.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  Future<List<String>> sources() async =>
      ((await _json('/api/sources'))['sources'] as List? ?? []).cast<String>();

  Future<List<Track>> search(String source, String q) =>
      _tracks('/api/search?source=$source&q=${Uri.encodeQueryComponent(q)}');

  Future<List<Track>> liked() => _tracks('/api/liked');
  Future<List<Track>> local() => _tracks('/api/local');
  Future<List<Track>> subStarred() => _tracks('/api/subsonic/starred');

  Future<List<String>> playlists() async =>
      ((await _json('/api/playlists'))['playlists'] as List? ?? [])
          .cast<String>();

  Future<List<Track>> playlist(String name) =>
      _tracks('/api/playlist?name=${Uri.encodeQueryComponent(name)}');

  /// streamUrl is the playable audio URL (token in query for the audio engine).
  String streamUrl(Track t) =>
      '$base/api/stream?id=${Uri.encodeQueryComponent(t.id)}&token=$token';

  /// artUri resolves a track's cover (public URL, or server-proxied with token).
  Uri? artUri(Track t) {
    final a = t.art;
    if (a == null || a.isEmpty) return null;
    if (a.startsWith('http')) return Uri.parse(a);
    final sep = a.contains('?') ? '&' : '?';
    return Uri.parse('$base$a${sep}token=$token');
  }
}
