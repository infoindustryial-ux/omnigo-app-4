import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:omnigo_app/core/services/payfast_service.dart';

void main() {
  group('PayfastService Unit Tests', () {
    test('Default configuration holds correct credentials and UAT settings', () {
      final service = PayfastService();

      expect(service.merchantId, equals('102'));
      expect(service.securedKey, equals('zWHjBp2AlttNu1sK'));
      expect(service.merchantName, equals('OMNIGO'));
      expect(service.isUAT, isTrue);
      expect(service.successUrl, equals('https://omnigo.com.pk/payment/success'));
      expect(service.failureUrl, equals('https://omnigo.com.pk/payment/failure'));
      expect(service.checkoutUrl, contains('/payments/payfast/ipn'));
    });

    test('Custom configuration overrides default credentials properly', () {
      final service = PayfastService(
        merchantId: '999',
        securedKey: 'custom_key_xyz',
        merchantName: 'OMNIGO LIVE',
        isUAT: false,
        checkoutUrl: 'https://custom-webhook.com/ipn',
      );

      expect(service.merchantId, equals('999'));
      expect(service.securedKey, equals('custom_key_xyz'));
      expect(service.merchantName, equals('OMNIGO LIVE'));
      expect(service.isUAT, isFalse);
      expect(service.checkoutUrl, equals('https://custom-webhook.com/ipn'));
    });

    test('PayfastPaymentResult creates success, failed, and cancelled instances', () {
      final successResult = PayfastPaymentResult.success('BASKET-123');
      expect(successResult.success, isTrue);
      expect(successResult.isCancelled, isFalse);
      expect(successResult.basketId, equals('BASKET-123'));
      expect(successResult.errorMessage, isNull);

      final failedResult = PayfastPaymentResult.failed('BASKET-123', errorMessage: 'Card declined');
      expect(failedResult.success, isFalse);
      expect(failedResult.isCancelled, isFalse);
      expect(failedResult.errorMessage, equals('Card declined'));

      final cancelledResult = PayfastPaymentResult.cancelled('BASKET-123');
      expect(cancelledResult.success, isFalse);
      expect(cancelledResult.isCancelled, isTrue);
      expect(cancelledResult.errorMessage, contains('cancelled'));
    });

    test('Validation hash matches PayFast official worked example', () {
      const basketId = 'BAS-01';
      const securedKey = 'jdnkaabcks';
      const merchantId = '102';
      const errCode = '000';

      final raw = '$basketId|$securedKey|$merchantId|$errCode';
      final hash = sha256.convert(utf8.encode(raw)).toString();

      expect(
        hash,
        equals('e8192a7554dd699975adf39619c703a492392edf5e416a61e183866ecdf6a2a2'),
      );
    });
  });
}
