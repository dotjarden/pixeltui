import 'package:flutter/cupertino.dart';

import '../api.dart';
import '../models.dart';
import '../theme.dart';
import '../widgets.dart';

/// SearchTab: a search field + source selector + results.
class SearchTab extends StatefulWidget {
  final Api api;
  final EdgeInsets padding;
  const SearchTab(
      {super.key, required this.api, this.padding = EdgeInsets.zero});
  @override
  State<SearchTab> createState() => _SearchTabState();
}

class _SearchTabState extends State<SearchTab> {
  final _ctl = TextEditingController();
  List<String> _sources = const ['youtube'];
  int _seg = 0;
  List<Track> _results = const [];
  bool _loading = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _loadSources();
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

  Future<void> _search() async {
    final q = _ctl.text.trim();
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
    const labels = {
      'youtube': 'YouTube',
      'subsonic': 'Subsonic',
      'local': 'Local'
    };
    return Column(
        children: [
          SizedBox(height: widget.padding.top + 8),
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 4, 16, 8),
            child: CupertinoSearchTextField(
              controller: _ctl,
              onSubmitted: (_) => _search(),
              style: const TextStyle(color: kText),
            ),
          ),
          if (_segSources.length > 1)
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 2),
              child: CupertinoSlidingSegmentedControl<int>(
                groupValue: _seg.clamp(0, _segSources.length - 1),
                children: {
                  for (var i = 0; i < _segSources.length; i++)
                    i: Padding(
                      padding:
                          const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                      child: Text(labels[_segSources[i]] ?? _segSources[i]),
                    ),
                },
                onValueChanged: (v) {
                  if (v != null) setState(() => _seg = v);
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
