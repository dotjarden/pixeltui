import 'package:adaptive_platform_ui/adaptive_platform_ui.dart';
import 'package:flutter/cupertino.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import '../api.dart';
import '../store.dart';
import 'home_screen.dart';

/// PairScreen scans the QR from `pixeltui serve` (or accepts manual entry) and
/// saves the device token.
class PairScreen extends StatefulWidget {
  const PairScreen({super.key});
  @override
  State<PairScreen> createState() => _PairScreenState();
}

class _PairScreenState extends State<PairScreen> {
  bool _busy = false;
  bool _handled = false;
  String? _error;

  Future<void> _pair(String url, String code) async {
    if (_busy) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final token = await Api.pair(url, code);
      final base = url.endsWith('/') ? url.substring(0, url.length - 1) : url;
      await Store.save(base, token);
      if (!mounted) return;
      Navigator.of(context).pushReplacement(
          CupertinoPageRoute(builder: (_) => const HomeScreen()));
    } catch (e) {
      setState(() {
        _error = '$e';
        _busy = false;
        _handled = false;
      });
    }
  }

  void _onDetect(BarcodeCapture cap) {
    if (_handled) return;
    for (final b in cap.barcodes) {
      final u = Uri.tryParse(b.rawValue ?? '');
      final url = u?.queryParameters['url'];
      final code = u?.queryParameters['code'];
      if (url != null && code != null) {
        _handled = true;
        _pair(url, code);
        return;
      }
    }
  }

  Future<void> _manual() async {
    final res = await Navigator.of(context).push<List<String>>(
        CupertinoPageRoute(builder: (_) => const _ManualPair()));
    if (res != null && res.length == 2) _pair(res[0], res[1]);
  }

  @override
  Widget build(BuildContext context) {
    return AdaptiveScaffold(
      appBar: AdaptiveAppBar(title: 'Pair with pixeltui'),
      body: SafeArea(
        child: Column(
          children: [
            Expanded(child: MobileScanner(onDetect: _onDetect)),
            Padding(
              padding: const EdgeInsets.all(16),
              child: Column(
                children: [
                  const Text('Scan the QR shown by “pixeltui serve”',
                      textAlign: TextAlign.center),
                  const SizedBox(height: 10),
                  if (_busy) const CupertinoActivityIndicator(),
                  if (_error != null)
                    Padding(
                      padding: const EdgeInsets.only(top: 8),
                      child: Text(_error!,
                          style: const TextStyle(
                              color: CupertinoColors.systemRed)),
                    ),
                  const SizedBox(height: 8),
                  AdaptiveButton(
                    onPressed: _manual,
                    label: 'Enter manually',
                    style: AdaptiveButtonStyle.tinted,
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _ManualPair extends StatefulWidget {
  const _ManualPair();
  @override
  State<_ManualPair> createState() => _ManualPairState();
}

class _ManualPairState extends State<_ManualPair> {
  String _url = '';
  String _code = '';

  @override
  Widget build(BuildContext context) {
    return AdaptiveScaffold(
      appBar: AdaptiveAppBar(title: 'Manual pairing'),
      body: SafeArea(
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            children: [
              AdaptiveTextField(
                placeholder: 'Server URL (http://…:8787)',
                onChanged: (v) => _url = v,
              ),
              const SizedBox(height: 12),
              AdaptiveTextField(
                placeholder: 'Code',
                onChanged: (v) => _code = v,
              ),
              const SizedBox(height: 20),
              AdaptiveButton(
                onPressed: () =>
                    Navigator.of(context).pop([_url.trim(), _code.trim()]),
                label: 'Pair',
                style: AdaptiveButtonStyle.filled,
              ),
            ],
          ),
        ),
      ),
    );
  }
}
