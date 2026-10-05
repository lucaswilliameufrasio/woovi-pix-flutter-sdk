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

sealed class _OrderState {
  const _OrderState();
}

final class _OrderIdle extends _OrderState {
  const _OrderIdle();
}

final class _OrderLoading extends _OrderState {
  const _OrderLoading();
}

final class _OrderError extends _OrderState {
  const _OrderError(this.message);
  final String message;
}

final class _OrderCheckout extends _OrderState {
  const _OrderCheckout(this.controller, this.transport);
  final CheckoutController controller;
  final HttpCheckoutTransport transport;
}

class _OrderPageState extends State<OrderPage> {
  static const _sandbox = bool.fromEnvironment('WOOVI_SANDBOX');
  static const _sandboxToken = String.fromEnvironment('SANDBOX_SESSION_TOKEN');
  static const _idempotencyKey =
      String.fromEnvironment('SANDBOX_IDEMPOTENCY_KEY');
  static const _configuredBackend = String.fromEnvironment('DEMO_BACKEND');
  static String get _backend => _configuredBackend.isNotEmpty
      ? _configuredBackend
      : (Platform.isAndroid ? 'http://10.0.2.2:8080' : 'http://127.0.0.1:8080');
  final _client = http.Client();
  _OrderState _state = const _OrderIdle();

  Future<void> _startCheckout() async {
    if (_state is _OrderLoading || _state is _OrderCheckout) {
      return;
    }
    setState(() => _state = const _OrderLoading());
    try {
      if (_sandbox &&
          (_sandboxToken.length < 32 ||
              _idempotencyKey.length < 8 ||
              _idempotencyKey.length > 128)) {
        throw StateError(
            'Configure SANDBOX_SESSION_TOKEN e SANDBOX_IDEMPOTENCY_KEY; preserve a chave após erros e reinícios.');
      }
      final response = await _client
          .post(
              Uri.parse(
                  '$_backend/v1/${_sandbox ? 'merchant/' : ''}checkout-sessions'),
              headers: {
                'Content-Type': 'application/json',
                if (_sandbox) 'X-Sandbox-Session': _sandboxToken,
                if (_sandbox) 'Idempotency-Key': _idempotencyKey
              },
              body: jsonEncode(
                  {'order_id': _sandbox ? 'sandbox-order-1' : 'demo-order-1'}))
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
      if (!mounted) {
        return;
      }
      final transport =
          HttpCheckoutTransport(baseUrl: Uri.parse(_backend), client: _client);
      final controller = CheckoutController(
        session: session,
        transport: transport,
      );
      setState(() => _state = _OrderCheckout(controller, transport));
    } catch (error) {
      if (mounted) {
        setState(() => _state =
            _OrderError('Não foi possível iniciar o checkout: $error'));
      }
    }
  }

  @override
  void dispose() {
    if (_state case _OrderCheckout(:final controller, :final transport)) {
      controller.dispose();
      transport.close();
    }
    _client.close();
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
                    subtitle: Text(_sandbox
                        ? 'Pedido sandbox-order-1 · R\$ 25,99'
                        : 'Pedido demo-order-1 · R\$ 25,99'))),
            const SizedBox(height: 16),
            if (_state case _OrderCheckout(:final controller)) ...[
              PixCheckoutView(
                controller: controller,
                onPaid: () => ScaffoldMessenger.of(context).showSnackBar(
                  const SnackBar(
                      content: Text(
                          'Status confirmado no backend demo; consulte o pedido no servidor.')),
                ),
              ),
              const SizedBox(height: 24),
              const Text(_sandbox
                  ? 'Simule o pagamento na conta de teste Woovi; o backend consulta o status via GET.'
                  : 'Para simular o pagamento, envie POST /v1/demo/checkouts/{checkout_id}/pay ao backend local.'),
            ] else ...[
              const Text(_sandbox
                  ? 'Woovi sandbox: somente conta e credenciais de teste. Não pague com banco real.'
                  : 'Exemplo local com um PSP simulado. Nenhuma cobrança real será criada.'),
              FilledButton(
                  onPressed: _state is _OrderLoading ? null : _startCheckout,
                  child: Text(_state is _OrderLoading
                      ? 'Carregando…'
                      : 'Pagar com Pix')),
              if (_state case _OrderError(:final message))
                Text(message,
                    style:
                        TextStyle(color: Theme.of(context).colorScheme.error)),
            ],
          ],
        ),
      );
}
