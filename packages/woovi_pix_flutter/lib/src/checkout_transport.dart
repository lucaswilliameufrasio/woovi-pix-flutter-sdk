import 'dart:convert';

import 'package:http/http.dart' as http;

import 'checkout_session.dart';

abstract interface class CheckoutTransport {
  Future<CheckoutSnapshot> fetchStatus(CheckoutSession session);
}

/// HTTP adapter for the merchant-owned, read-only checkout status endpoint.
class HttpCheckoutTransport implements CheckoutTransport {
  HttpCheckoutTransport({
    required this.baseUrl,
    http.Client? client,
    this.requestTimeout = const Duration(seconds: 10),
  })  : _client = client ?? http.Client(),
        _ownsClient = client == null;

  final Uri baseUrl;
  final Duration requestTimeout;
  final http.Client _client;
  final bool _ownsClient;
  bool _closed = false;

  /// Stops this transport. Closes only an internally created client.
  /// The caller retains ownership of an injected client and its in-flight work.
  void close() {
    if (_closed) {
      return;
    }
    _closed = true;
    if (_ownsClient) {
      _client.close();
    }
  }

  @override
  Future<CheckoutSnapshot> fetchStatus(CheckoutSession session) async {
    if (_closed) {
      throw StateError('Checkout transport is closed');
    }
    final base = baseUrl.toString().endsWith('/')
        ? baseUrl
        : Uri.parse('${baseUrl.toString()}/');
    final uri =
        base.resolve('v1/checkout-sessions/${Uri.encodeComponent(session.id)}');
    final response = await _client.get(uri, headers: {
      'Authorization': 'Bearer ${session.accessToken}',
      'Accept': 'application/json'
    }).timeout(requestTimeout);
    if (_closed) {
      throw StateError('Checkout transport is closed');
    }
    if (response.statusCode != 200) {
      Object? decodedError;
      try {
        decodedError = jsonDecode(response.body);
      } on FormatException {
        decodedError = null;
      }
      final body = decodedError is Map<String, Object?> ? decodedError : null;
      final extra = body?['extra'];
      throw CheckoutTransportException(
        statusCode: response.statusCode,
        errorCode: body?['error_code'] is String
            ? body!['error_code']! as String
            : 'UNEXPECTED_ERROR',
        message: body?['message'] is String
            ? body!['message']! as String
            : 'Falha ao consultar o checkout',
        extra: extra is Map<String, Object?> ? extra : null,
      );
    }
    final decoded = jsonDecode(response.body);
    if (decoded is! Map<String, Object?>) {
      throw const FormatException('Invalid status response');
    }
    return CheckoutSnapshot.fromJson(decoded);
  }
}

class CheckoutTransportException implements Exception {
  const CheckoutTransportException({
    required this.statusCode,
    required this.errorCode,
    required this.message,
    this.extra,
  });

  final int statusCode;
  final String errorCode;
  final String message;
  final Map<String, Object?>? extra;
  @override
  String toString() => '$errorCode ($statusCode): $message';
}
