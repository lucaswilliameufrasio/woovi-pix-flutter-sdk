import 'dart:convert';
import 'dart:io';
import 'dart:math';

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
  const _OrderCheckout(
      this.controller, this.transport, this.orderId, this.idempotencyKey);
  final CheckoutController controller;
  final HttpCheckoutTransport transport;
  final String orderId;
  final String idempotencyKey;
}

class _OrderPageState extends State<OrderPage> {
  static const _sandbox = bool.fromEnvironment('WOOVI_SANDBOX');
  static const _sandboxToken = String.fromEnvironment('SANDBOX_SESSION_TOKEN');
  static const _idempotencyKey =
      String.fromEnvironment('SANDBOX_IDEMPOTENCY_KEY');
  static const _configuredSandboxOrderId = String.fromEnvironment(
    'SANDBOX_ORDER_ID',
    defaultValue: 'sandbox-order-1',
  );
  static const _configuredBackend = String.fromEnvironment('DEMO_BACKEND');
  static String get _backend => _configuredBackend.isNotEmpty
      ? _configuredBackend
      : (Platform.isAndroid ? 'http://10.0.2.2:8080' : 'http://127.0.0.1:8080');
  final _client = http.Client();
  String _currentOrderId = _configuredSandboxOrderId;
  String _currentIdempotencyKey = _idempotencyKey;
  _OrderState _state = const _OrderIdle();

  Future<void> _startCheckout() async {
    if (_state is _OrderLoading || _state is _OrderCheckout) {
      return;
    }
    setState(() => _state = const _OrderLoading());
    await _requestCheckout();
  }

  Future<void> _startNewSandboxRound() async {
    if (!_sandbox || _state is _OrderLoading) {
      return;
    }
    _disposeCurrentCheckout();
    setState(() => _state = const _OrderLoading());
    try {
      if (_sandboxToken.length < 32) {
        throw StateError('Configure SANDBOX_SESSION_TOKEN para o sandbox.');
      }
      final orderResponse = await _client.post(
        Uri.parse('$_backend/v1/sandbox/test-orders'),
        headers: {'X-Sandbox-Session': _sandboxToken},
      ).timeout(const Duration(seconds: 10));
      if (orderResponse.statusCode != 201) {
        throw StateError(
            'Não foi possível criar um pedido de teste: ${orderResponse.statusCode} ${orderResponse.body}');
      }
      final orderBody = jsonDecode(orderResponse.body);
      if (orderBody is! Map<String, Object?> ||
          orderBody['order_id'] is! String ||
          orderBody['amount_cents'] != 2599) {
        throw const FormatException('Invalid sandbox test order response');
      }
      _currentOrderId = orderBody['order_id']! as String;
      _currentIdempotencyKey = _newIdempotencyKey();
      await _requestCheckout();
    } catch (error) {
      if (mounted) {
        setState(() => _state =
            _OrderError('Não foi possível iniciar outra rodada: $error'));
      }
    }
  }

  Future<void> _requestCheckout() async {
    try {
      if (_sandbox &&
          (_sandboxToken.length < 32 ||
              _currentIdempotencyKey.length < 8 ||
              _currentIdempotencyKey.length > 128)) {
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
                if (_sandbox) 'Idempotency-Key': _currentIdempotencyKey
              },
              body: jsonEncode(
                  {'order_id': _sandbox ? _currentOrderId : 'demo-order-1'}))
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
      setState(() => _state = _OrderCheckout(
          controller, transport, _currentOrderId, _currentIdempotencyKey));
    } catch (error) {
      if (mounted) {
        setState(() => _state =
            _OrderError('Não foi possível iniciar o checkout: $error'));
      }
    }
  }

  String _newIdempotencyKey() {
    final random = Random.secure();
    final value = StringBuffer();
    for (var index = 0; index < 16; index++) {
      value.write(random.nextInt(256).toRadixString(16).padLeft(2, '0'));
    }
    return value.toString();
  }

  void _disposeCurrentCheckout() {
    if (_state case _OrderCheckout(:final controller, :final transport)) {
      controller.dispose();
      transport.close();
    }
  }

  @override
  void dispose() {
    _disposeCurrentCheckout();
    _client.close();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Scaffold(
        appBar: AppBar(title: const Text('Pedido de demonstração')),
        body: ListView(
          padding: const EdgeInsets.all(24),
          children: [
            Card(
                child: ListTile(
                    leading: Icon(Icons.inventory_2),
                    title: Text('Caneca fictícia'),
                    subtitle: Text(_sandbox
                        ? 'Pedido $_currentOrderId · R\$ 25,99'
                        : 'Pedido demo-order-1 · R\$ 25,99'))),
            const SizedBox(height: 16),
            if (_state
                case _OrderCheckout(
                  :final controller,
                  :final orderId,
                  :final idempotencyKey,
                )) ...[
              PixCheckoutView(
                controller: controller,
                onPaid: () => ScaffoldMessenger.of(context).showSnackBar(
                  SnackBar(
                    content: Text(_sandbox
                        ? 'Pagamento confirmado pela Woovi sandbox.'
                        : 'Pagamento simulado confirmado pelo backend local.'),
                  ),
                ),
              ),
              const SizedBox(height: 24),
              if (_sandbox) ...[
                Text(
                    'Abra $_backend/sandbox para confirmar o pagamento de teste ou acionar cenários locais.'),
                SelectableText(
                    'Para recuperar esta rodada: $orderId · Idempotency-Key $idempotencyKey'),
                if (controller.status != CheckoutStatus.pending)
                  FilledButton.icon(
                    onPressed: _startNewSandboxRound,
                    icon: const Icon(Icons.refresh),
                    label: const Text('Iniciar nova rodada sandbox'),
                  ),
              ] else ...[
                const Text(
                    'Para simular o pagamento, use POST /v1/demo/checkouts/{checkout_id}/pay no backend local.'),
                FilledButton.icon(
                  onPressed: () {
                    _disposeCurrentCheckout();
                    setState(() => _state = const _OrderIdle());
                    _startCheckout();
                  },
                  icon: const Icon(Icons.refresh),
                  label: const Text('Iniciar novo checkout local'),
                ),
              ],
            ] else ...[
              const Text(_sandbox
                  ? 'Woovi sandbox: somente conta e credenciais de teste. Não pague com banco real.'
                  : 'Exemplo local com um PSP simulado. Nenhuma cobrança real será criada.'),
              FilledButton(
                  onPressed: _state is _OrderLoading ? null : _startCheckout,
                  child: Text(_state is _OrderLoading
                      ? 'Carregando…'
                      : 'Pagar com Pix')),
              if (_state case _OrderError(:final message)) ...[
                Text(message,
                    style:
                        TextStyle(color: Theme.of(context).colorScheme.error)),
                if (_sandbox)
                  SelectableText(
                      'Recuperação: $_currentOrderId · Idempotency-Key $_currentIdempotencyKey'),
                if (_sandbox)
                  FilledButton.icon(
                    onPressed: _startNewSandboxRound,
                    icon: const Icon(Icons.refresh),
                    label: const Text('Iniciar nova rodada sandbox'),
                  ),
              ],
            ],
          ],
        ),
      );
}
