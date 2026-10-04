import 'dart:convert';

import 'package:http/http.dart' as http;

import 'checkout_session.dart';

abstract interface class CheckoutTransport {
  Future<CheckoutSnapshot> fetchStatus(CheckoutSession session);
}

/// HTTP adapter for the merchant-owned, read-only checkout status endpoint.
class HttpCheckoutTransport implements CheckoutTransport {
  HttpCheckoutTransport({required this.baseUrl, http.Client? client})
      : _client = client ?? http.Client();

  final Uri baseUrl;
  final http.Client _client;

  @override
  Future<CheckoutSnapshot> fetchStatus(CheckoutSession session) async {
    final base = baseUrl.toString().endsWith('/')
        ? baseUrl
        : Uri.parse('${baseUrl.toString()}/');
    final uri =
        base.resolve('v1/checkout-sessions/${Uri.encodeComponent(session.id)}');
    final response = await _client.get(uri, headers: {
      'Authorization': 'Bearer ${session.accessToken}',
      'Accept': 'application/json'
    }).timeout(const Duration(seconds: 10));
    if (response.statusCode != 200) {
      throw CheckoutTransportException(
          'Status request failed (${response.statusCode})');
    }
    final decoded = jsonDecode(response.body);
    if (decoded is! Map<String, Object?>) {
      throw const FormatException('Invalid status response');
    }
    return CheckoutSnapshot.fromJson(decoded);
  }
}

class CheckoutTransportException implements Exception {
  const CheckoutTransportException(this.message);
  final String message;
  @override
  String toString() => message;
}
