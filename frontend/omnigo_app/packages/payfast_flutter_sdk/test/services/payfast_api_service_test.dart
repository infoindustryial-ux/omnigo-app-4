import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:payfast_flutter_sdk/payfast_flutter_sdk.dart';

import '../support/fake_http_adapter.dart';

void main() {
  late Dio dio;
  late FakeHttpAdapter adapter;

  PayfastApiService serviceWith(
    ResponseBody Function(RequestOptions options) respond, {
    bool isUAT = true,
  }) {
    adapter = FakeHttpAdapter(respond);
    dio = Dio()..httpClientAdapter = adapter;
    return PayfastApiService(dio: dio, isUAT: isUAT);
  }

  group('PayfastApiService.getToken', () {
    test('returns the token on a 200 response', () async {
      final service = serviceWith(
        (_) => jsonResponse({'ACCESS_TOKEN': 'tok_123', 'STATUS': '00'}),
      );

      final response = await service.getToken(
        merchantId: 'MID-123',
        securedKey: 'SECRET-abc',
      );

      expect(response.token, 'tok_123');
      expect(response.status, '00');
    });

    test('posts form-url-encoded credentials to the UAT endpoint', () async {
      final service = serviceWith((_) => jsonResponse({'ACCESS_TOKEN': 'tok'}));

      await service.getToken(merchantId: 'MID-123', securedKey: 'SECRET-abc');

      expect(
        adapter.lastRequest!.uri.toString(),
        'https://ipguat.apps.net.pk/Ecommerce/api/Transaction/GetAccessToken',
      );
      expect(adapter.lastRequest!.method, 'POST');
      expect(
        adapter.lastRequest!.headers[Headers.contentTypeHeader],
        contains(Headers.formUrlEncodedContentType),
      );
      expect(adapter.lastRequestBody, contains('MERCHANT_ID=MID-123'));
      expect(adapter.lastRequestBody, contains('SECURED_KEY=SECRET-abc'));
    });

    test('targets the production endpoint when isUAT is false', () async {
      final service = serviceWith(
        (_) => jsonResponse({'ACCESS_TOKEN': 'tok'}),
        isUAT: false,
      );

      await service.getToken(merchantId: 'MID-123', securedKey: 'SECRET-abc');

      expect(
        adapter.lastRequest!.uri.toString(),
        'https://ipg1.apps.net.pk/Ecommerce/api/Transaction/GetAccessToken',
      );
    });

    test('surfaces a gateway failure payload instead of throwing', () async {
      final service = serviceWith(
        (_) => jsonResponse({'STATUS': '01', 'MESSAGE': 'Invalid credentials'}),
      );

      final response = await service.getToken(
        merchantId: 'MID-123',
        securedKey: 'wrong',
      );

      expect(response.token, isNull);
      expect(response.message, 'Invalid credentials');
    });

    test('throws when the gateway returns a non-2xx status', () async {
      final service = serviceWith(
        (_) => jsonResponse({'MESSAGE': 'Server error'}, statusCode: 500),
      );

      expect(
        () => service.getToken(merchantId: 'MID-123', securedKey: 'SECRET-abc'),
        throwsA(isA<Exception>()),
      );
    });

    test('throws when the transport fails', () async {
      final service = serviceWith((options) {
        throw DioException.connectionError(
          requestOptions: options,
          reason: 'no route to host',
        );
      });

      expect(
        () => service.getToken(merchantId: 'MID-123', securedKey: 'SECRET-abc'),
        throwsA(isA<Exception>()),
      );
    });
  });

  test('dispose closes the underlying client', () {
    final service = serviceWith((_) => jsonResponse({'ACCESS_TOKEN': 'tok'}));

    service.dispose();

    expect(adapter.closed, isTrue);
  });
}
