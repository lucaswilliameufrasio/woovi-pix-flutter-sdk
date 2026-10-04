import 'package:flutter_test/flutter_test.dart';
import 'package:woovi_pix_flutter_example/main.dart';

void main() {
  testWidgets('shows fictional order and checkout action', (tester) async {
    await tester.pumpWidget(const DemoApp());
    expect(find.text('Caneca fictícia'), findsOneWidget);
    expect(find.text('Pagar com Pix'), findsOneWidget);
  });
}
