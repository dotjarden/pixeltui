import 'package:flutter/material.dart';

import '../api.dart';
import '../audio.dart';
import '../models.dart';
import '../store.dart';
import 'pair_screen.dart';
import 'player_screen.dart';

/// HomeScreen: pick a source, search/browse, tap to play. A mini-player sits at
/// the bottom and opens the full Now-Playing screen.
class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key});
  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  Api? _api;
  String _source = 'youtube';
  List<String> _sources = ['youtube'];
  List<Track> _tracks = [];
  bool _loading = false;
  String? _error;
  final _searchCtl = TextEditingController();

  @override
  void initState() {
    super.initState();
    _init();
  }

  Future<void> _init() async {
    final c = await Store.load();
    if (c == null) {
      _logout();
      return;
    }
    final api = Api(c.url, c.token);
    setState(() => _api = api);
    try {
      final s = await api.sources();
      if (mounted) setState(() => _sources = s);
    } catch (_) {}
    _browse();
  }

  Future<void> _run(Future<List<Track>> Function() f) async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final t = await f();
      if (mounted) {
        setState(() {
          _tracks = t;
          _loading = false;
        });
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _error = '$e';
          _loading = false;
        });
      }
    }
  }

  void _browse() {
    final api = _api;
    if (api == null) return;
    switch (_source) {
      case 'liked':
        _run(api.liked);
        break;
      case 'local':
        _run(api.local);
        break;
      case 'subsonic':
        _run(api.subStarred);
        break;
      default:
        setState(() => _tracks = []); // youtube: needs a search
    }
  }

  void _search() {
    final api = _api;
    if (api == null) return;
    final q = _searchCtl.text.trim();
    if (q.isEmpty) return;
    final src = (_source == 'subsonic' || _source == 'local') ? _source : 'youtube';
    _run(() => api.search(src, q));
  }

  Future<void> _logout() async {
    await Store.clear();
    if (!mounted) return;
    Navigator.of(context)
        .pushReplacement(MaterialPageRoute(builder: (_) => const PairScreen()));
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('pixeltui'),
        actions: [
          IconButton(onPressed: _logout, icon: const Icon(Icons.logout)),
        ],
      ),
      body: Column(
        children: [
          Padding(
            padding: const EdgeInsets.all(8),
            child: Row(
              children: [
                DropdownButton<String>(
                  value: _source,
                  items: _sourceItems(),
                  onChanged: (v) {
                    if (v != null) {
                      setState(() => _source = v);
                      _browse();
                    }
                  },
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: TextField(
                    controller: _searchCtl,
                    textInputAction: TextInputAction.search,
                    onSubmitted: (_) => _search(),
                    decoration: const InputDecoration(
                      hintText: 'Search…',
                      isDense: true,
                      border: OutlineInputBorder(),
                    ),
                  ),
                ),
                IconButton(onPressed: _search, icon: const Icon(Icons.search)),
              ],
            ),
          ),
          if (_loading) const LinearProgressIndicator(),
          if (_error != null)
            Padding(
              padding: const EdgeInsets.all(8),
              child: Text(_error!,
                  style: const TextStyle(color: Colors.redAccent)),
            ),
          Expanded(
            child: ListView.builder(
              itemCount: _tracks.length,
              itemBuilder: (context, i) {
                final t = _tracks[i];
                return ListTile(
                  leading: _art(t),
                  title: Text(t.title,
                      maxLines: 1, overflow: TextOverflow.ellipsis),
                  subtitle: Text(t.artist,
                      maxLines: 1, overflow: TextOverflow.ellipsis),
                  onTap: () =>
                      AudioController.instance.playAll(_api!, _tracks, i),
                );
              },
            ),
          ),
          const MiniPlayer(),
        ],
      ),
    );
  }

  List<DropdownMenuItem<String>> _sourceItems() {
    const labels = {
      'youtube': 'YouTube',
      'subsonic': 'Subsonic',
      'local': 'Local',
      'liked': 'Liked',
    };
    final items = <String>['youtube'];
    if (_sources.contains('subsonic')) items.add('subsonic');
    if (_sources.contains('local')) items.add('local');
    items.add('liked');
    return items
        .map((s) => DropdownMenuItem(value: s, child: Text(labels[s] ?? s)))
        .toList();
  }

  Widget _art(Track t) {
    final u = _api?.artUri(t);
    if (u == null) {
      return const SizedBox(width: 44, height: 44, child: Icon(Icons.music_note));
    }
    return SizedBox(
      width: 44,
      height: 44,
      child: Image.network(u.toString(),
          fit: BoxFit.cover,
          errorBuilder: (_, __, ___) => const Icon(Icons.music_note)),
    );
  }
}
