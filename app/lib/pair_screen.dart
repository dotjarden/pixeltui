import 'package:flutter/cupertino.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import 'api.dart';
import 'root_shell.dart';
import 'store.dart';
import 'theme.dart';

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
          CupertinoPageRoute(builder: (_) => const RootShell()));
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
    return CupertinoPageScaffold(
      backgroundColor: kBg,
      child: SafeArea(
        child: Column(
          children: [
            const Padding(
              padding: EdgeInsets.fromLTRB(16, 16, 16, 8),
              child: Text('Pair with pixeltui',
                  style: TextStyle(
                      color: kText, fontSize: 26, fontWeight: FontWeight.bold)),
            ),
            Expanded(
              child: Container(
                margin: const EdgeInsets.symmetric(horizontal: 16),
                clipBehavior: Clip.antiAlias,
                decoration: BoxDecoration(borderRadius: BorderRadius.circular(16)),
                child: MobileScanner(onDetect: _onDetect),
              ),
            ),
            Padding(
              padding: const EdgeInsets.all(16),
              child: Column(
                children: [
                  const Text('Scan the QR shown by “pixeltui serve”',
                      textAlign: TextAlign.center,
                      style: TextStyle(color: kMuted)),
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
                  CupertinoButton(
                    color: kCard2,
                    onPressed: _manual,
                    child: const Text('Enter manually',
                        style: TextStyle(color: kText)),
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
  final _url = TextEditingController();
  final _code = TextEditingController();

  @override
  Widget build(BuildContext context) {
    return CupertinoPageScaffold(
      backgroundColor: kBg,
      navigationBar: const CupertinoNavigationBar(
        backgroundColor: kBg,
        border: null,
        middle: Text('Manual pairing', style: TextStyle(color: kText)),
      ),
      child: SafeArea(
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            children: [
              CupertinoTextField(
                controller: _url,
                placeholder: 'Server URL (http://…:8787)',
                style: const TextStyle(color: kText),
              ),
              const SizedBox(height: 12),
              CupertinoTextField(
                controller: _code,
                placeholder: 'Code',
                style: const TextStyle(color: kText),
              ),
              const SizedBox(height: 20),
              SizedBox(
                width: double.infinity,
                child: CupertinoButton.filled(
                  onPressed: () => Navigator.of(context)
                      .pop([_url.text.trim(), _code.text.trim()]),
                  child: const Text('Pair'),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
