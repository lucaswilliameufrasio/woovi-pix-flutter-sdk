import 'dart:async';
import 'dart:math';

import 'package:flutter/widgets.dart';

import 'checkout_session.dart';
import 'checkout_transport.dart';

class CheckoutController extends ChangeNotifier with WidgetsBindingObserver {
  CheckoutController({
    required this.session,
    required CheckoutTransport transport,
    this.pollInterval = const Duration(seconds: 3),
    Random? random,
  })  : _transport = transport,
        _random = random ?? Random(),
        _status = session.status {
    WidgetsBinding.instance.addObserver(this);
    _schedule(Duration.zero);
  }

  final CheckoutSession session;
  final CheckoutTransport _transport;
  final Random _random;
  final Duration pollInterval;
  Timer? _timer;
  bool _disposed = false;
  bool _inFlight = false;
  bool _paused = false;
  int _failures = 0;
  CheckoutStatus _status;
  Object? _transportError;

  CheckoutStatus get status => _status;
  Object? get transportError => _transportError;
  bool get isRefreshing => _inFlight;

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      _paused = false;
      unawaited(refresh());
    } else if (state == AppLifecycleState.paused ||
        state == AppLifecycleState.inactive ||
        state == AppLifecycleState.detached) {
      _paused = true;
      _timer?.cancel();
      _timer = null;
    }
  }

  Future<void> refresh() async {
    if (_disposed || _paused || _inFlight) return;
    _timer?.cancel();
    _timer = null;
    _inFlight = true;
    notifyListeners();
    try {
      final latest = await _transport.fetchStatus(session);
      if (_disposed) return;
      if (!(_status == CheckoutStatus.paid &&
          latest.status != CheckoutStatus.paid)) {
        _status = latest.status;
      }
      _transportError = null;
      _failures = 0;
    } catch (error) {
      if (_disposed) return;
      _transportError = error;
      _failures = min(_failures + 1, 5);
    } finally {
      _inFlight = false;
      if (!_disposed) {
        notifyListeners();
        if (_status == CheckoutStatus.pending) _schedule(_nextDelay());
      }
    }
  }

  Duration _nextDelay() {
    if (_failures == 0) return pollInterval;
    final cap = min(pollInterval.inMilliseconds * (1 << _failures), 60000);
    return Duration(
        milliseconds: (cap * (0.75 + _random.nextDouble() * 0.5)).round());
  }

  void _schedule(Duration delay) {
    _timer?.cancel();
    if (_disposed || _paused || _status != CheckoutStatus.pending) return;
    _timer = Timer(delay, () => unawaited(refresh()));
  }

  @override
  void dispose() {
    _disposed = true;
    _timer?.cancel();
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }
}
