import 'package:flutter_secure_storage/flutter_secure_storage.dart';

/// Creds is the paired server URL + device token (saved in the Keychain).
class Creds {
  final String url;
  final String token;
  const Creds(this.url, this.token);
}

/// Store persists the pairing in secure storage.
class Store {
  static const _s = FlutterSecureStorage();

  static Future<Creds?> load() async {
    final url = await _s.read(key: 'url');
    final token = await _s.read(key: 'token');
    if (url == null || token == null) return null;
    return Creds(url, token);
  }

  static Future<void> save(String url, String token) async {
    await _s.write(key: 'url', value: url);
    await _s.write(key: 'token', value: token);
  }

  static Future<void> clear() => _s.deleteAll();
}
