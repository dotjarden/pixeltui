import 'dart:async';

import 'package:adaptive_platform_ui/adaptive_platform_ui.dart';
import 'package:flutter/cupertino.dart';

import '../api.dart';
import '../models.dart';
import '../theme.dart';
import '../widgets.dart';

/// SearchTab: a modern adaptive search field + segmented source control with
/// live (debounced) results.
class SearchTab extends StatefulWidget {
  final Api api;
  final EdgeInsets padding;
  const SearchTab(
      {super.key, required this.api, this.padding = EdgeInsets.zero});
  @override
  State<SearchTab> createState() => _SearchTabState();
}

class _SearchTabState extends State<SearchTab> {
  List<String> _sources = const ['youtube'];
  int _seg = 0;
  String _query = '';
  Timer? _debounce;
  List<Track> _results = const [];
  bool _loading = false;
  String? _error;

  static const _labels = {
    'youtube': 'YouTube',
    'subsonic': 'Subsonic',
    'local': 'Local',
  };

  @override
  void initState() {
    super.initState();
    _loadSources();
  }

  @override
  void dispose() {
    _debounce?.cancel();
    super.dispose();
  }

  Future<void> _loadSources() async {
    try {
      final s = await widget.api.sources();
      if (mounted) setState(() => _sources = s);
    } catch (_) {}
  }

  List<String> get _segSources {
    final out = <String>['youtube'];
    if (_sources.contains('subsonic')) out.add('subsonic');
    if (_sources.contains('local')) out.add('local');
    return out;
  }

  void _onChanged(String v) {
    _query = v;
    _debounce?.cancel();
    if (v.trim().isEmpty) {
      setState(() => _results = const []);
      return;
    }
    _debounce = Timer(const Duration(milliseconds: 450), _search);
  }

  Future<void> _search() async {
    final q = _query.trim();
    if (q.isEmpty) return;
    final src = _segSources[_seg.clamp(0, _segSources.length - 1)];
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final r = await widget.api.search(src, q);
      if (mounted) setState(() {
        _results = r;
        _loading = false;
      });
    } catch (e) {
      if (mounted) setState(() {
        _error = '$e';
        _loading = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        SizedBox(height: widget.padding.top + 4),
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 4, 16, 8),
          child: AdaptiveTextField(
            placeholder: 'Artists, songs, videos',
            prefixIcon: const Icon(CupertinoIcons.search, size: 18),
            onChanged: _onChanged,
          ),
        ),
        if (_segSources.length > 1)
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 2),
            child: AdaptiveSegmentedControl(
              labels: [for (final s in _segSources) _labels[s] ?? s],
              selectedIndex: _seg.clamp(0, _segSources.length - 1),
              onValueChanged: (i) {
                setState(() => _seg = i);
                _search();
              },
            ),
          ),
        if (_loading)
          const Padding(
              padding: EdgeInsets.all(16),
              child: CupertinoActivityIndicator()),
        if (_error != null)
          Padding(
            padding: const EdgeInsets.all(16),
            child: Text(_error!,
                style: const TextStyle(color: CupertinoColors.systemRed)),
          ),
        Expanded(
          child: (_results.isEmpty && !_loading)
              ? const Center(
                  child:
                      Text('Search your music', style: TextStyle(color: kMuted)))
              : ListView.builder(
                  padding: EdgeInsets.only(bottom: widget.padding.bottom),
                  itemCount: _results.length,
                  itemBuilder: (c, i) => TrackTile(
                    track: _results[i],
                    api: widget.api,
                    onTap: () => playList(widget.api, _results, i),
                  ),
                ),
        ),
      ],
    );
  }
}
