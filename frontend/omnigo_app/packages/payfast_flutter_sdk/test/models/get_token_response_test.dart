import 'package:flutter_test/flutter_test.dart';
import 'package:payfast_flutter_sdk/payfast_flutter_sdk.dart';

void main() {
  group('GetTokenResponse.fromJson', () {
    test('maps the gateway success payload', () {
      final response = GetTokenResponse.fromJson({
        'ACCESS_TOKEN': 'tok_123',
        'STATUS': '00',
        'MESSAGE': 'Success',
      });

      expect(response.token, 'tok_123');
      expect(response.status, '00');
      expect(response.message, 'Success');
    });

    test('maps a failure payload with no token', () {
      final response = GetTokenResponse.fromJson({
        'STATUS': '01',
        'MESSAGE': 'Invalid merchant credentials',
      });

      expect(response.token, isNull);
      expect(response.status, '01');
      expect(response.message, 'Invalid merchant credentials');
    });

    test('tolerates an empty payload', () {
      final response = GetTokenResponse.fromJson({});

      expect(response.token, isNull);
      expect(response.status, isNull);
      expect(response.message, isNull);
    });
  });

  group('GetTokenResponse.toJson', () {
    test('round-trips a full response', () {
      final json = GetTokenResponse.fromJson({
        'ACCESS_TOKEN': 'tok_123',
        'STATUS': '00',
        'MESSAGE': 'Success',
      }).toJson();

      expect(json, {
        'ACCESS_TOKEN': 'tok_123',
        'STATUS': '00',
        'MESSAGE': 'Success',
      });
    });

    test('omits null fields', () {
      final json = GetTokenResponse(status: '01').toJson();

      expect(json, {'STATUS': '01'});
    });
  });
}
