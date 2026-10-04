import 'package:flutter_test/flutter_test.dart';
import 'package:woovi_pix_flutter/woovi_pix_flutter.dart';

class _SequenceTransport implements CheckoutTransport {
  _SequenceTransport(this.results);
  final List<Object> results;

  @override
  Future<CheckoutSnapshot> fetchStatus(CheckoutSession session) async {
    final result = results.removeAt(0);
    if (result is Exception) throw result;
    return CheckoutSnapshot(
        status: result as CheckoutStatus, expiresAt: session.expiresAt);
  }
}

CheckoutSession _session() => CheckoutSession(
      id: 'checkout-1',
      accessToken: 'opaque',
      status: CheckoutStatus.pending,
      amountCents: 1250,
      currency: 'BRL',
      expiresAt: DateTime.utc(2030),
      pixCopyPaste: 'pix-demo',
    );

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('transport errors never become a payment status', () async {
    final controller = CheckoutController(
      session: _session(),
      transport: _SequenceTransport([Exception('offline')]),
    );
    await controller.refresh();
    expect(controller.status, CheckoutStatus.pending);
    expect(controller.transportError, isNotNull);
    controller.dispose();
  });

  test('paid is terminal even if a later response is stale', () async {
    final controller = CheckoutController(
      session: _session(),
      transport:
          _SequenceTransport([CheckoutStatus.paid, CheckoutStatus.pending]),
    );
    await controller.refresh();
    expect(controller.status, CheckoutStatus.paid);
    await controller.refresh();
    expect(controller.status, CheckoutStatus.paid);
    controller.dispose();
  });

  test('expired can be corrected to paid for a tardy Pix', () async {
    final session = CheckoutSession(
      id: 'checkout-1',
      accessToken: 'opaque',
      status: CheckoutStatus.expired,
      amountCents: 1250,
      currency: 'BRL',
      expiresAt: DateTime.utc(2030),
      pixCopyPaste: 'pix-demo',
    );
    final controller = CheckoutController(
      session: session,
      transport: _SequenceTransport([CheckoutStatus.paid]),
    );
    await controller.refresh();
    expect(controller.status, CheckoutStatus.paid);
    controller.dispose();
  });
}
