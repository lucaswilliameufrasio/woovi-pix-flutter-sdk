import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:qr_flutter/qr_flutter.dart';

import 'checkout_controller.dart';
import 'checkout_session.dart';

class PixCheckoutView extends StatefulWidget {
  const PixCheckoutView({super.key, required this.controller, this.onPaid});

  final CheckoutController controller;
  final VoidCallback? onPaid;

  @override
  State<PixCheckoutView> createState() => _PixCheckoutViewState();
}

class _PixCheckoutViewState extends State<PixCheckoutView> {
  bool _notifiedPaid = false;

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
        animation: widget.controller,
        builder: (context, _) {
          final controller = widget.controller;
          if (controller.status == CheckoutStatus.paid && !_notifiedPaid) {
            _notifiedPaid = true;
            WidgetsBinding.instance.addPostFrameCallback((_) {
              if (mounted) widget.onPaid?.call();
            });
          }
          final session = controller.session;
          final pixCopyPaste = session.pixCopyPaste;
          final statusLabel = switch (controller.status) {
            CheckoutStatus.pending => 'Aguardando pagamento',
            CheckoutStatus.paid => 'Pagamento confirmado pelo servidor',
            CheckoutStatus.expired => 'Cobrança expirada',
          };
          return Semantics(
            liveRegion: true,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text(
                    'Pague R\$ ${(session.amountCents / 100).toStringAsFixed(2)}',
                    style: Theme.of(context).textTheme.headlineSmall),
                Text(statusLabel),
                const SizedBox(height: 16),
                if (controller.status == CheckoutStatus.pending &&
                    pixCopyPaste != null) ...[
                  Center(
                      child: QrImageView(
                          data: pixCopyPaste,
                          size: 220,
                          semanticsLabel: 'QR Code Pix')),
                  SelectableText(pixCopyPaste),
                  TextButton.icon(
                    onPressed: () =>
                        Clipboard.setData(ClipboardData(text: pixCopyPaste)),
                    icon: const Icon(Icons.copy),
                    label: const Text('Copiar código Pix'),
                  ),
                  Text('Válido até ${session.expiresAt.toLocal()}'),
                ],
                if (controller.status == CheckoutStatus.pending &&
                    pixCopyPaste == null)
                  const Text('Aguardando os dados Pix do servidor.'),
                if (controller.transportError != null)
                  Text(
                      'Não foi possível consultar. O pagamento continua sem confirmação.',
                      style: TextStyle(
                          color: Theme.of(context).colorScheme.error)),
                if (controller.transportError != null ||
                    controller.status == CheckoutStatus.pending)
                  OutlinedButton(
                      onPressed: controller.refresh,
                      child: const Text('Consultar novamente')),
              ],
            ),
          );
        },
      );
}
