import 'package:flutter/material.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import '../api.dart';
import '../store.dart';
import 'home_screen.dart';

/// PairScreen scans the QR printed by `pixeltui serve` (or accepts manual entry)
/// and saves the device token.
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
          MaterialPageRoute(builder: (_) => const HomeScreen()));
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
      final raw = b.rawValue;
      if (raw == null) continue;
      final u = Uri.tryParse(raw);
      final url = u?.queryParameters['url'];
      final code = u?.queryParameters['code'];
      if (url != null && code != null) {
        _handled = true;
        _pair(url, code);
        return;
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Pair with pixeltui')),
      body: Column(
        children: [
          Expanded(child: MobileScanner(onDetect: _onDetect)),
          Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              children: [
                const Text('Scan the QR shown by `pixeltui serve`',
                    textAlign: TextAlign.center),
                if (_busy)
                  const Padding(
                      padding: EdgeInsets.only(top: 12),
                      child: CircularProgressIndicator()),
                if (_error != null)
                  Padding(
                    padding: const EdgeInsets.only(top: 12),
                    child: Text(_error!,
                        style: const TextStyle(color: Colors.redAccent)),
                  ),
                TextButton(
                  onPressed: _busy ? null : _manual,
                  child: const Text('Enter manually'),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  void _manual() {
    final url = TextEditingController();
    final code = TextEditingController();
    showDialog(
      context: context,
      builder: (_) => AlertDialog(
        title: const Text('Manual pairing'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              controller: url,
              decoration: const InputDecoration(
                  labelText: 'Server URL', hintText: 'http://…:8787'),
            ),
            TextField(
              controller: code,
              decoration: const InputDecoration(labelText: 'Code'),
            ),
          ],
        ),
        actions: [
          TextButton(
              onPressed: () => Navigator.pop(context),
              child: const Text('Cancel')),
          TextButton(
            onPressed: () {
              Navigator.pop(context);
              _pair(url.text.trim(), code.text.trim());
            },
            child: const Text('Pair'),
          ),
        ],
      ),
    );
  }
}
