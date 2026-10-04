enum CheckoutStatus { pending, paid, expired }

/// Checkout data issued by the merchant backend, never by the mobile app.
class CheckoutSession {
  const CheckoutSession({
    required this.id,
    required this.accessToken,
    required this.status,
    required this.amountCents,
    required this.currency,
    required this.expiresAt,
    required this.pixCopyPaste,
  });

  final String id;
  final String accessToken;
  final CheckoutStatus status;
  final int amountCents;
  final String currency;
  final DateTime expiresAt;
  final String pixCopyPaste;

  factory CheckoutSession.fromJson(Map<String, Object?> json) {
    final id = _requiredString(json, 'checkout_id');
    final token = _requiredString(json, 'access_token');
    final currency = _requiredString(json, 'currency');
    final copyPaste = _requiredString(json, 'pix_copy_paste');
    final cents = json['amount_cents'];
    if (id.length > 128 ||
        token.length > 4096 ||
        currency != 'BRL' ||
        copyPaste.length > 4096 ||
        cents is! int ||
        cents < 0 ||
        cents > 100000000) {
      throw const FormatException('Invalid checkout fields');
    }
    final expiry = DateTime.tryParse(_requiredString(json, 'expires_at'));
    if (expiry == null) {
      throw const FormatException('Invalid expires_at');
    }
    return CheckoutSession(
      id: id,
      accessToken: token,
      status: _parseStatus(json['status']),
      amountCents: cents,
      currency: currency,
      expiresAt: expiry.toUtc(),
      pixCopyPaste: copyPaste,
    );
  }

  static String _requiredString(Map<String, Object?> json, String key) {
    final value = json[key];
    if (value is! String || value.isEmpty) {
      throw FormatException('Invalid $key');
    }
    return value;
  }

  static CheckoutStatus _parseStatus(Object? value) => switch (value) {
        'pending' => CheckoutStatus.pending,
        'paid' => CheckoutStatus.paid,
        'expired' => CheckoutStatus.expired,
        _ => throw const FormatException('Unknown checkout status'),
      };
}

class CheckoutSnapshot {
  const CheckoutSnapshot({required this.status, required this.expiresAt});

  final CheckoutStatus status;
  final DateTime expiresAt;

  factory CheckoutSnapshot.fromJson(Map<String, Object?> json) {
    final expiryValue = json['expires_at'];
    if (expiryValue is! String) {
      throw const FormatException('Invalid expires_at');
    }
    final expiry = DateTime.tryParse(expiryValue);
    if (expiry == null) {
      throw const FormatException('Invalid expires_at');
    }
    return CheckoutSnapshot(
      status: CheckoutSession._parseStatus(json['status']),
      expiresAt: expiry.toUtc(),
    );
  }
}
