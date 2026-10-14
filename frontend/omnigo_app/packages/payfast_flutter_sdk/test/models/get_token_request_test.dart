import 'package:flutter_test/flutter_test.dart';
import 'package:payfast_flutter_sdk/payfast_flutter_sdk.dart';

void main() {
  group('GetTokenRequest', () {
    test('toFormData uses the gateway field names', () {
      final request = GetTokenRequest(
        merchantId: 'MID-123',
        securedKey: 'SECRET-abc',
      );

      expect(request.toFormData(), {
        'MERCHANT_ID': 'MID-123',
        'SECURED_KEY': 'SECRET-abc',
      });
    });
  });
}
