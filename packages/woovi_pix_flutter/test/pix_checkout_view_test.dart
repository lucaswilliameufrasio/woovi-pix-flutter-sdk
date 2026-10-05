import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:qr_flutter/qr_flutter.dart';
import 'package:woovi_pix_flutter/woovi_pix_flutter.dart';

class _StatusTransport implements CheckoutTransport {
  Object result = CheckoutStatus.pending;

  @override
  Future<CheckoutSnapshot> fetchStatus(CheckoutSession session) async {
    final value = result;
    if (value is Exception) {
      throw value;
    }
    return CheckoutSnapshot(
        status: value as CheckoutStatus, expiresAt: session.expiresAt);
  }
}

class _PendingTransport implements CheckoutTransport {
  final response = Completer<CheckoutSnapshot>();

  @override
  Future<CheckoutSnapshot> fetchStatus(CheckoutSession session) =>
      response.future;
}

CheckoutSession _session(String id, CheckoutStatus status) => CheckoutSession(
      id: id,
      accessToken: 'test-only',
      status: status,
      amountCents: 2599,
      currency: 'BRL',
      expiresAt: DateTime.utc(2030),
      pixCopyPaste: status == CheckoutStatus.paid ? null : 'pix-test',
    );

Widget _view(CheckoutController controller, VoidCallback onPaid) => MaterialApp(
      home: Scaffold(
        body: PixCheckoutView(controller: controller, onPaid: onPaid),
      ),
    );

void main() {
  testWidgets('a failed refresh does not cast doubt on a confirmed payment',
      (tester) async {
    final transport = _StatusTransport()..result = Exception('offline');
    final controller = CheckoutController(
      session: _session('paid', CheckoutStatus.paid),
      transport: transport,
    );
    addTearDown(controller.dispose);
    await tester.pumpWidget(_view(controller, () {}));
    await controller.refresh();
    await tester.pump();
    expect(find.text('Pagamento confirmado pelo servidor'), findsOneWidget);
    expect(find.textContaining('O pagamento continua sem confirmação'),
        findsNothing);
  });

  testWidgets('refresh is disabled while waiting for the server',
      (tester) async {
    final transport = _PendingTransport();
    final controller = CheckoutController(
      session: _session('pending', CheckoutStatus.pending),
      transport: transport,
    );
    addTearDown(controller.dispose);
    final refresh = controller.refresh();
    await tester.pumpWidget(_view(controller, () {}));
    await tester.pump();
    expect(find.text('Consultando…'), findsOneWidget);
    expect(tester.widget<OutlinedButton>(find.byType(OutlinedButton)).onPressed,
        isNull);
    transport.response.complete(CheckoutSnapshot(
        status: CheckoutStatus.pending, expiresAt: DateTime.utc(2030)));
    await refresh;
    await tester.pump();
    expect(find.text('Consultar novamente'), findsOneWidget);
    await tester.pumpWidget(const SizedBox.shrink());
    controller.dispose();
  });

  testWidgets('paid transition calls once and unmount suppresses notification',
      (tester) async {
    final transport = _StatusTransport();
    final controller = CheckoutController(
      session: _session('pending', CheckoutStatus.pending),
      transport: transport,
    );
    addTearDown(controller.dispose);
    var calls = 0;
    await tester.pumpWidget(_view(controller, () => calls++));
    transport.result = CheckoutStatus.paid;
    await controller.refresh();
    await tester.pump();
    await tester.pumpWidget(_view(controller, () => calls++));
    expect(calls, 1);
    expect(find.byType(QrImageView), findsNothing);
    await tester.pumpWidget(const SizedBox.shrink());
    await controller.refresh();
    await tester.pump();
    expect(calls, 1);
  });

  testWidgets('notifies once per controller, including a replacement',
      (tester) async {
    final first = CheckoutController(
      session: _session('first', CheckoutStatus.paid),
      transport: _StatusTransport(),
    );
    final second = CheckoutController(
      session: _session('second', CheckoutStatus.paid),
      transport: _StatusTransport(),
    );
    addTearDown(first.dispose);
    addTearDown(second.dispose);
    var calls = 0;
    void onPaid() => calls++;
    await tester.pumpWidget(_view(first, onPaid));
    await tester.pumpWidget(_view(first, onPaid));
    expect(calls, 1);
    await tester.pumpWidget(_view(second, onPaid));
    expect(calls, 2);
    expect(find.byType(QrImageView), findsNothing);
    expect(find.text('Copiar código Pix'), findsNothing);
  });

  testWidgets('copies Pix, preserves pending on error, hides QR when expired',
      (tester) async {
    final transport = _StatusTransport();
    final controller = CheckoutController(
      session: _session('pending', CheckoutStatus.pending),
      transport: transport,
    );
    addTearDown(controller.dispose);
    String? copied;
    tester.binding.defaultBinaryMessenger
        .setMockMethodCallHandler(SystemChannels.platform, (call) async {
      if (call.method == 'Clipboard.setData') {
        copied = (call.arguments as Map)['text'] as String;
      }
      return null;
    });
    addTearDown(() => tester.binding.defaultBinaryMessenger
        .setMockMethodCallHandler(SystemChannels.platform, null));
    var paidCalls = 0;
    await tester.pumpWidget(_view(controller, () => paidCalls++));
    expect(find.byType(QrImageView), findsOneWidget);
    await tester.tap(find.text('Copiar código Pix'));
    await tester.pump();
    expect(copied, 'pix-test');
    transport.result = Exception('offline');
    await controller.refresh();
    await tester.pump();
    expect(find.textContaining('O pagamento continua sem confirmação'),
        findsOneWidget);
    expect(find.byType(QrImageView), findsOneWidget);
    expect(paidCalls, 0);
    transport.result = CheckoutStatus.expired;
    await tester.tap(find.text('Consultar novamente'));
    await tester.pump();
    expect(find.text('Cobrança expirada'), findsOneWidget);
    expect(find.byType(QrImageView), findsNothing);
    expect(find.text('Copiar código Pix'), findsNothing);
    expect(paidCalls, 0);
    await tester.pumpWidget(const SizedBox.shrink());
  });
}
