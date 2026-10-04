import 'package:flutter_test/flutter_test.dart';
import 'package:woovi_pix_flutter_example/main.dart';

void main() {
  testWidgets('shows fictional order and checkout action', (tester) async {
    await tester.pumpWidget(const DemoApp());
    expect(find.text('Caneca fictícia'), findsOneWidget);
    expect(find.text('Pagar com Pix'), findsOneWidget);
    const sandbox = bool.fromEnvironment('WOOVI_SANDBOX');
    expect(
      find.text(sandbox
          ? 'Pedido sandbox-order-1 · R\$ 25,99'
          : 'Pedido demo-order-1 · R\$ 25,99'),
      findsOneWidget,
    );
  });
  testWidgets('sandbox missing config is rejected before network access',
      (tester) async {
    const sandbox = bool.fromEnvironment('WOOVI_SANDBOX');
    if (!sandbox) return;
    await tester.pumpWidget(const DemoApp());
    await tester.tap(find.text('Pagar com Pix'));
    await tester.pumpAndSettle();
    expect(
        find.textContaining('Configure SANDBOX_SESSION_TOKEN'), findsOneWidget);
  });
}
