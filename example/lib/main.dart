import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:woovi_pix_flutter/woovi_pix_flutter.dart';

void main() => runApp(const DemoApp());

class DemoApp extends StatelessWidget {
  const DemoApp({super.key});

  @override
  Widget build(BuildContext context) => MaterialApp(
        title: 'Pix Checkout Demo',
        theme: ThemeData(
            colorSchemeSeed: const Color(0xff087f5b), useMaterial3: true),
        home: const OrderPage(),
      );
}

class OrderPage extends StatefulWidget {
  const OrderPage({super.key});

  @override
  State<OrderPage> createState() => _OrderPageState();
}

class _OrderPageState extends State<OrderPage> {
  static const _configuredBackend = String.fromEnvironment('DEMO_BACKEND');
  static String get _backend => _configuredBackend.isNotEmpty
      ? _configuredBackend
      : (Platform.isAndroid ? 'http://10.0.2.2:8080' : 'http://127.0.0.1:8080');
  CheckoutController? _controller;
  bool _loading = false;
  String? _error;

  Future<void> _startCheckout() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final response = await http
          .post(Uri.parse('$_backend/v1/checkout-sessions'),
              headers: {'Content-Type': 'application/json'},
              body: jsonEncode({'order_id': 'demo-order-1'}))
          .timeout(const Duration(seconds: 10));
      if (response.statusCode != 200 && response.statusCode != 201) {
        Object? decodedError;
        try {
          decodedError = jsonDecode(response.body);
        } on FormatException {
          decodedError = null;
        }
        final errorBody =
            decodedError is Map<String, Object?> ? decodedError : null;
        final errorCode = errorBody?['error_code'] is String
            ? errorBody!['error_code']! as String
            : 'UNEXPECTED_ERROR';
        final message = errorBody?['message'] is String
            ? errorBody!['message']! as String
            : 'Não foi possível iniciar o checkout';
        throw StateError('$errorCode: $message');
      }
      final body = jsonDecode(response.body);
      if (body is! Map<String, Object?>) {
        throw const FormatException('Invalid backend response');
      }
      final session = CheckoutSession.fromJson(body);
      final controller = CheckoutController(
        session: session,
        transport: HttpCheckoutTransport(baseUrl: Uri.parse(_backend)),
      );
      if (!mounted) {
        controller.dispose();
        return;
      }
      _controller?.dispose();
      setState(() => _controller = controller);
    } catch (error) {
      if (mounted) {
        setState(() => _error = 'Não foi possível iniciar o checkout: $error');
      }
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  @override
  void dispose() {
    _controller?.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Scaffold(
        appBar: AppBar(title: const Text('Pedido de demonstração')),
        body: ListView(
          padding: const EdgeInsets.all(24),
          children: [
            const Card(
                child: ListTile(
                    leading: Icon(Icons.inventory_2),
                    title: Text('Caneca fictícia'),
                    subtitle: Text('Pedido demo-order-1 · R\$ 25,99'))),
            const SizedBox(height: 16),
            if (_controller == null) ...[
              const Text(
                  'Exemplo local com um PSP simulado. Nenhuma cobrança real será criada.'),
              FilledButton(
                  onPressed: _loading ? null : _startCheckout,
                  child: Text(_loading ? 'Carregando…' : 'Pagar com Pix')),
              if (_error != null)
                Text(_error!,
                    style:
                        TextStyle(color: Theme.of(context).colorScheme.error)),
            ] else ...[
              PixCheckoutView(
                controller: _controller!,
                onPaid: () => ScaffoldMessenger.of(context).showSnackBar(
                  const SnackBar(
                      content: Text(
                          'Status confirmado no backend demo; consulte o pedido no servidor.')),
                ),
              ),
              const SizedBox(height: 24),
              const Text(
                  'Para simular o pagamento, envie POST /v1/demo/checkouts/{checkout_id}/pay ao backend local.'),
            ],
          ],
        ),
      );
}
