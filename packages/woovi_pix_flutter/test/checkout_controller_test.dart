import 'dart:async';

import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:woovi_pix_flutter/woovi_pix_flutter.dart';

class _SequenceTransport implements CheckoutTransport {
  _SequenceTransport(this.results);
  final List<Object> results;

  @override
  Future<CheckoutSnapshot> fetchStatus(CheckoutSession session) async {
    final result = results.removeAt(0);
    if (result is Exception) {
      throw result;
    }
    return CheckoutSnapshot(
        status: result as CheckoutStatus, expiresAt: session.expiresAt);
  }
}

class _PendingTransport implements CheckoutTransport {
  final response = Completer<CheckoutSnapshot>();
  int calls = 0;

  @override
  Future<CheckoutSnapshot> fetchStatus(CheckoutSession session) {
    calls++;
    return response.future;
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

  test('concurrent refreshes share one request and ignore disposal completion',
      () async {
    final transport = _PendingTransport();
    final controller =
        CheckoutController(session: _session(), transport: transport);
    var notifications = 0;
    controller.addListener(() => notifications++);
    final first = controller.refresh();
    expect(controller.state, isA<CheckoutRefreshing>());
    expect(controller.isRefreshing, isTrue);
    await controller.refresh();
    expect(transport.calls, 1);
    controller.dispose();
    transport.response.complete(CheckoutSnapshot(
        status: CheckoutStatus.paid, expiresAt: _session().expiresAt));
    await first;
    expect(controller.state, isA<CheckoutDisposed>());
    expect(controller.status, CheckoutStatus.pending);
    expect(controller.isRefreshing, isFalse);
    expect(notifications, 1);
    await controller.refresh();
    expect(transport.calls, 1);
  });

  test('pausing blocks requests and resume refreshes once', () async {
    final transport = _PendingTransport();
    final controller =
        CheckoutController(session: _session(), transport: transport);
    controller.didChangeAppLifecycleState(AppLifecycleState.paused);
    await controller.refresh();
    expect(transport.calls, 0);
    controller.didChangeAppLifecycleState(AppLifecycleState.resumed);
    expect(transport.calls, 1);
    controller.didChangeAppLifecycleState(AppLifecycleState.resumed);
    expect(transport.calls, 1);
    controller.dispose();
    transport.response.completeError(Exception('closed'));
    await Future<void>.delayed(Duration.zero);
    expect(controller.state, isA<CheckoutDisposed>());
  });

  test('failure payload is cleared by a successful retry', () async {
    final controller = CheckoutController(
      session: _session(),
      transport:
          _SequenceTransport([Exception('offline'), CheckoutStatus.pending]),
    );
    addTearDown(controller.dispose);
    await controller.refresh();
    expect(controller.state, isA<CheckoutFailure>());
    expect((controller.state as CheckoutFailure).failures, 1);
    await controller.refresh();
    expect(controller.state, isA<CheckoutReady>());
    expect(controller.transportError, isNull);
    expect(controller.status, CheckoutStatus.pending);
  });

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
