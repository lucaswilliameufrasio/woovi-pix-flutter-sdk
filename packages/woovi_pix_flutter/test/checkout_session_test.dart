import 'package:flutter_test/flutter_test.dart';
import 'package:woovi_pix_flutter/woovi_pix_flutter.dart';

void main() {
  test('parses a valid server-issued checkout', () {
    final session = CheckoutSession.fromJson({
      'checkout_id': 'checkout-1',
      'access_token': 'opaque-secret',
      'status': 'pending',
      'amount_cents': 1250,
      'currency': 'BRL',
      'expires_at': '2030-01-01T00:00:00Z',
      'pix_copy_paste': '000201-demo',
    });
    expect(session.amountCents, 1250);
    expect(session.status, CheckoutStatus.pending);
  });

  test('rejects unknown payment states', () {
    expect(
      () => CheckoutSession.fromJson({
        'checkout_id': 'checkout-1',
        'access_token': 'token',
        'status': 'failed',
        'amount_cents': 100,
        'currency': 'BRL',
        'expires_at': '2030-01-01T00:00:00Z',
        'pix_copy_paste': 'pix',
      }),
      throwsFormatException,
    );
  });

  test('parses paid checkout without a Pix payload', () {
    final session = CheckoutSession.fromJson({
      'checkout_id': 'checkout-paid',
      'access_token': 'opaque-secret',
      'status': 'paid',
      'amount_cents': 100,
      'currency': 'BRL',
      'expires_at': '2030-01-01T00:00:00Z',
    });
    expect(session.status, CheckoutStatus.paid);
    expect(session.pixCopyPaste, isNull);
  });

  test('requires a Pix payload for a pending checkout', () {
    expect(
      () => CheckoutSession.fromJson({
        'checkout_id': 'checkout-pending',
        'access_token': 'opaque-secret',
        'status': 'pending',
        'amount_cents': 100,
        'currency': 'BRL',
        'expires_at': '2030-01-01T00:00:00Z',
      }),
      throwsFormatException,
    );
  });
}
