import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:woovi_pix_flutter/woovi_pix_flutter.dart';

void main() {
  test('surfaces stable house error_code separately from human message',
      () async {
    final transport = HttpCheckoutTransport(
      baseUrl: Uri.parse('https://merchant.example/api/'),
      client: MockClient((request) async {
        expect(request.url.path, '/api/v1/checkout-sessions/checkout-1');
        return http.Response(
          '{"message":"Checkout não encontrado","error_code":"CHECKOUT_NOT_FOUND"}',
          404,
          headers: {'content-type': 'application/json'},
        );
      }),
    );

    await expectLater(
      transport.fetchStatus(_session()),
      throwsA(
        isA<CheckoutTransportException>()
            .having((error) => error.statusCode, 'statusCode', 404)
            .having(
                (error) => error.errorCode, 'errorCode', 'CHECKOUT_NOT_FOUND')
            .having(
                (error) => error.message, 'message', 'Checkout não encontrado'),
      ),
    );
  });
}

CheckoutSession _session() => CheckoutSession(
      id: 'checkout-1',
      accessToken: 'bearer',
      status: CheckoutStatus.pending,
      amountCents: 2599,
      currency: 'BRL',
      expiresAt: DateTime.utc(2030),
      pixCopyPaste: 'pix-value',
    );
