import 'dart:async';
import 'dart:math';

import 'package:flutter/widgets.dart';

import 'checkout_session.dart';
import 'checkout_transport.dart';

/// Request state carries the last server-confirmed payment status.
/// Transport failures never masquerade as a payment outcome.
sealed class CheckoutState {
  const CheckoutState(this.status);
  final CheckoutStatus status;
}

final class CheckoutReady extends CheckoutState {
  const CheckoutReady(super.status);
}

final class CheckoutRefreshing extends CheckoutState {
  const CheckoutRefreshing(super.status, {required this.previousFailures});
  final int previousFailures;
}

final class CheckoutFailure extends CheckoutState {
  const CheckoutFailure(super.status, this.error, {required this.failures});
  final Object error;
  final int failures;
}

final class CheckoutDisposed extends CheckoutState {
  const CheckoutDisposed(super.status);
}

/// Borrows [transport]; its owner must close any HTTP resources after disposal.
class CheckoutController extends ChangeNotifier with WidgetsBindingObserver {
  CheckoutController({
    required this.session,
    required CheckoutTransport transport,
    this.pollInterval = const Duration(seconds: 3),
    Random? random,
  })  : _transport = transport,
        _random = random ?? Random(),
        _state = CheckoutReady(session.status) {
    WidgetsBinding.instance.addObserver(this);
    _schedule(Duration.zero);
  }

  final CheckoutSession session;
  final CheckoutTransport _transport;
  final Random _random;
  final Duration pollInterval;
  Timer? _timer;
  bool _paused = false;
  CheckoutState _state;

  CheckoutState get state => _state;
  CheckoutStatus get status => _state.status;
  Object? get transportError => switch (_state) {
        CheckoutFailure(:final error) => error,
        _ => null,
      };
  bool get isRefreshing => _state is CheckoutRefreshing;

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (_state is CheckoutDisposed) {
      return;
    }
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
    if (_state is CheckoutDisposed || _paused || isRefreshing) {
      return;
    }
    _timer?.cancel();
    _timer = null;
    final failures = switch (_state) {
      CheckoutFailure(:final failures) => failures,
      _ => 0,
    };
    _state = CheckoutRefreshing(status, previousFailures: failures);
    notifyListeners();
    if (_state is CheckoutDisposed) {
      return;
    }
    try {
      final latest = await _transport.fetchStatus(session);
      if (_state is CheckoutDisposed) {
        return;
      }
      final canTransition = switch (status) {
        CheckoutStatus.pending => true,
        CheckoutStatus.expired => latest.status == CheckoutStatus.paid,
        CheckoutStatus.paid => false,
      };
      _state = CheckoutReady(canTransition ? latest.status : status);
    } catch (error) {
      if (_state is CheckoutDisposed) {
        return;
      }
      _state = CheckoutFailure(status, error, failures: min(failures + 1, 5));
    } finally {
      if (_state is! CheckoutDisposed) {
        notifyListeners();
        if (status == CheckoutStatus.pending) {
          _schedule(_nextDelay());
        }
      }
    }
  }

  Duration _nextDelay() {
    final failures = switch (_state) {
      CheckoutFailure(:final failures) => failures,
      _ => 0,
    };
    if (failures == 0) {
      return pollInterval;
    }
    final cap = min(pollInterval.inMilliseconds * (1 << failures), 60000);
    return Duration(
        milliseconds: (cap * (0.75 + _random.nextDouble() * 0.5)).round());
  }

  void _schedule(Duration delay) {
    _timer?.cancel();
    _timer = null;
    if (_state is CheckoutDisposed ||
        _paused ||
        status != CheckoutStatus.pending) {
      return;
    }
    _timer = Timer(delay, () => unawaited(refresh()));
  }

  @override
  void dispose() {
    if (_state is CheckoutDisposed) {
      return;
    }
    _state = CheckoutDisposed(status);
    _timer?.cancel();
    _timer = null;
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }
}
