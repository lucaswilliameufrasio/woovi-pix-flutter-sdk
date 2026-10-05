import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:woovi_pix_flutter/woovi_pix_flutter.dart';

CheckoutSession _session() => CheckoutSession(
      id: 'checkout-1',
      accessToken: 'test-bearer',
      status: CheckoutStatus.pending,
      amountCents: 2599,
      currency: 'BRL',
      expiresAt: DateTime.utc(2030),
      pixCopyPaste: 'test-pix',
    );

void main() {
  late HttpServer server;
  late Uri baseUrl;
  late Future<void> Function(HttpRequest) respond;

  setUp(() async {
    server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    baseUrl = Uri.parse('http://127.0.0.1:${server.port}/api');
    respond = (request) async {
      request.response.headers.contentType = ContentType.json;
      request.response.write(jsonEncode({
        'status': 'paid',
        'expires_at': DateTime.utc(2030).toIso8601String(),
      }));
      await request.response.close();
    };
    server.listen((request) => unawaited(respond(request)));
  });
  tearDown(() async => server.close(force: true));

  test('real HTTP sends scoped bearer and parses a terminal status', () async {
    final original = respond;
    respond = (request) async {
      expect(request.method, 'GET');
      expect(request.uri.path, '/api/v1/checkout-sessions/checkout-1');
      expect(request.headers.value('authorization'), 'Bearer test-bearer');
      expect(request.headers.value('accept'), 'application/json');
      await original(request);
    };
    final transport = HttpCheckoutTransport(baseUrl: baseUrl);
    addTearDown(transport.close);
    final status = await transport.fetchStatus(_session());
    expect(status.status, CheckoutStatus.paid);
    transport.close();
    transport.close();
    await expectLater(transport.fetchStatus(_session()), throwsStateError);
  });

  test('closing transport leaves an injected real HTTP client usable',
      () async {
    final client = http.Client();
    addTearDown(client.close);
    final transport = HttpCheckoutTransport(baseUrl: baseUrl, client: client);
    addTearDown(transport.close);
    await transport.fetchStatus(_session());
    transport.close();
    expect((await client.get(baseUrl)).statusCode, 200);
    await expectLater(transport.fetchStatus(_session()), throwsStateError);
  });

  test('closing during an injected request rejects its late response',
      () async {
    final received = Completer<void>();
    final release = Completer<void>();
    final original = respond;
    respond = (request) async {
      received.complete();
      await release.future;
      await original(request);
    };
    final client = http.Client();
    addTearDown(client.close);
    final transport = HttpCheckoutTransport(baseUrl: baseUrl, client: client);
    addTearDown(transport.close);
    final request = transport.fetchStatus(_session());
    final assertion = expectLater(request, throwsStateError);
    await received.future;
    transport.close();
    release.complete();
    await assertion;
    respond = original;
    expect((await client.get(baseUrl)).statusCode, 200);
  });

  for (final payload in ['not-json', '[]', '{"status":"unknown"}']) {
    test('rejects malformed status payload: $payload', () async {
      respond = (request) async {
        request.response.write(payload);
        await request.response.close();
      };
      final transport = HttpCheckoutTransport(baseUrl: baseUrl);
      addTearDown(transport.close);
      await expectLater(
          transport.fetchStatus(_session()), throwsFormatException);
    });
  }

  test('non-JSON server errors use a stable generic error', () async {
    respond = (request) async {
      request.response.statusCode = 503;
      request.response.write('service unavailable');
      await request.response.close();
    };
    final transport = HttpCheckoutTransport(baseUrl: baseUrl);
    addTearDown(transport.close);
    await expectLater(
      transport.fetchStatus(_session()),
      throwsA(isA<CheckoutTransportException>()
          .having((error) => error.statusCode, 'statusCode', 503)
          .having((error) => error.errorCode, 'errorCode', 'UNEXPECTED_ERROR')),
    );
  });

  test('timeout never returns a payment outcome', () async {
    final received = Completer<void>();
    respond = (request) async => received.complete();
    final transport = HttpCheckoutTransport(
      baseUrl: baseUrl,
      requestTimeout: const Duration(milliseconds: 100),
    );
    addTearDown(transport.close);
    final request = transport.fetchStatus(_session());
    final assertion = expectLater(request, throwsA(isA<TimeoutException>()));
    await received.future;
    await assertion;
  });
}
